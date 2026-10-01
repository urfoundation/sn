package miner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/miner/onchain"
)

type ClaimDaemonConfig struct {
	SchemaVersion  int      `yaml:"schema_version" json:"schema_version"`
	Release        string   `yaml:"release" json:"release"`
	APIURL         string   `yaml:"api_url" json:"api_url"`
	RPC            []string `yaml:"rpc" json:"rpc"`
	KeyFile        string   `yaml:"key_file" json:"key_file"`
	JWTFile        string   `yaml:"jwt_file,omitempty" json:"jwt_file,omitempty"`
	StateDir       string   `yaml:"state_dir" json:"state_dir"`
	PollSeconds    int      `yaml:"poll_seconds" json:"poll_seconds"`
	LookbackEpochs uint64   `yaml:"lookback_epochs" json:"lookback_epochs"`
}

func LoadClaimDaemonConfig(path string) (*ClaimDaemonConfig, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	var cfg ClaimDaemonConfig
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return nil, fmt.Errorf("claim daemon config contains trailing YAML: %w", err)
		}
		return nil, errors.New("claim daemon config contains multiple YAML documents")
	}
	base := filepath.Dir(abs)
	for name, value := range map[string]*string{"key_file": &cfg.KeyFile, "state_dir": &cfg.StateDir} {
		if *value == "" {
			return nil, fmt.Errorf("%s is empty", name)
		}
		if !filepath.IsAbs(*value) {
			*value = filepath.Join(base, *value)
		}
		*value = filepath.Clean(*value)
	}
	if cfg.JWTFile != "" {
		if !filepath.IsAbs(cfg.JWTFile) {
			cfg.JWTFile = filepath.Join(base, cfg.JWTFile)
		}
		cfg.JWTFile = filepath.Clean(cfg.JWTFile)
	}
	if cfg.PollSeconds == 0 {
		cfg.PollSeconds = 30
	}
	if cfg.LookbackEpochs == 0 {
		cfg.LookbackEpochs = 2
	}
	if cfg.SchemaVersion != 1 || cfg.Release != "1.0" || cfg.APIURL == "" || len(cfg.RPC) == 0 || cfg.PollSeconds < 5 || cfg.PollSeconds > 3600 || cfg.LookbackEpochs > 256 {
		return nil, errors.New("invalid release-1.0 claim daemon configuration")
	}
	if _, err := os.Stat(cfg.KeyFile); err != nil {
		return nil, fmt.Errorf("claim relayer key: %w", err)
	}
	if cfg.JWTFile != "" {
		info, err := os.Stat(cfg.JWTFile)
		if err != nil {
			return nil, fmt.Errorf("claim network JWT: %w", err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			return nil, errors.New("claim network JWT must be a private regular file")
		}
	}
	return &cfg, nil
}

type ClaimQueueEntry struct {
	Epoch              int64  `json:"epoch"`
	Status             string `json:"status"`
	Attempts           int    `json:"attempts"`
	ReconcileAttempts  int    `json:"reconcile_attempts,omitempty"`
	UpdatedAt          string `json:"updated_at"`
	NextRetryAt        string `json:"next_retry_at,omitempty"`
	TxHash             string `json:"tx_hash,omitempty"`
	RawTxHex           string `json:"raw_tx_hex,omitempty"`
	FinalizedBlock     uint64 `json:"finalized_block,omitempty"`
	FinalizedBlockHash string `json:"finalized_block_hash,omitempty"`
	ReceiptStatus      uint64 `json:"receipt_status,omitempty"`
	ReceiptLogsHash    string `json:"receipt_logs_sha256,omitempty"`
	LastError          string `json:"last_error,omitempty"`
}

type ClaimQueue struct {
	Schema         string                      `json:"schema"`
	LastDiscovered int64                       `json:"last_discovered"`
	Entries        map[string]*ClaimQueueEntry `json:"entries"`
}

// One daemon owns this store. Only a successful file and directory sync can
// establish the same-process acknowledgement used for unchanged saves.
type claimQueueStore struct {
	path      string
	savedHash [sha256.Size]byte
	saved     bool
}

func newClaimQueueStore(stateDir string) (*claimQueueStore, error) {
	if !filepath.IsAbs(stateDir) {
		return nil, errors.New("claim queue state_dir must be absolute")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Stat(stateDir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("claim queue state_dir %s must have mode 0700 or stricter", stateDir)
	}
	return &claimQueueStore{path: filepath.Join(stateDir, "claim-queue.json")}, nil
}

func (s *claimQueueStore) load() (*ClaimQueue, error) {
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return &ClaimQueue{Schema: "urnetwork-provider-claim-queue-v1", LastDiscovered: -1, Entries: map[string]*ClaimQueueEntry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var q ClaimQueue
	if err := json.Unmarshal(b, &q); err != nil {
		return nil, err
	}
	if q.Schema != "urnetwork-provider-claim-queue-v1" || q.Entries == nil {
		return nil, errors.New("invalid claim queue schema")
	}
	// The child writes the exact signed RLP before broadcast. A submitting
	// record with those bytes is recoverable; without them, the child had not
	// yet reached the broadcast boundary and a fresh attempt is safe.
	for _, entry := range q.Entries {
		if entry == nil {
			return nil, errors.New("claim queue contains a nil entry")
		}
		if entry.Status == "submitting" {
			if entry.RawTxHex != "" || entry.TxHash != "" {
				entry.Status = "uncertain"
				entry.LastError = "process restarted after exact transaction preparation; automatic canonical reconciliation pending"
			} else {
				entry.Status = "retry"
				entry.LastError = "process restarted before the durable broadcast boundary"
			}
		}
	}
	return &q, nil
}

// Compare the current durable bytes, not a process-local cache: unchanged
// discovery polls need no rename/fsync, and removed files must be recreated.
func (self *claimQueueStore) save(q *ClaimQueue) error {
	b, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	hash := sha256.Sum256(b)
	if self.saved && self.savedHash == hash {
		info, statErr := os.Lstat(self.path)
		if statErr == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0o600 {
			prior, readErr := os.ReadFile(self.path)
			if readErr == nil && bytes.Equal(prior, b) {
				return nil
			}
			if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
				self.saved = false
				return readErr
			}
		}
		self.saved = false
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	// A failed rename or directory sync must not make the next identical
	// attempt skip the durability boundary merely because bytes are visible.
	self.saved = false
	f, err := os.CreateTemp(filepath.Dir(self.path), ".claim-queue-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, self.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(self.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return err
	}
	self.savedHash, self.saved = hash, true
	return nil
}

func claimRetry(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := time.Minute << min(attempt-1, 6)
	if d > time.Hour {
		return time.Hour
	}
	return d
}

func discoverClaims(q *ClaimQueue, current int64, lookback uint64) {
	lastClaimable := current - 1
	if lastClaimable < 0 {
		return
	}
	start := q.LastDiscovered + 1
	if q.LastDiscovered < 0 {
		start = lastClaimable - int64(lookback) + 1
		if start < 0 {
			start = 0
		}
	}
	for epoch := start; epoch <= lastClaimable; epoch++ {
		key := fmt.Sprint(epoch)
		if q.Entries[key] == nil {
			q.Entries[key] = &ClaimQueueEntry{Epoch: epoch, Status: "pending", UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		}
		q.LastDiscovered = epoch
	}
}

func claimTxHash(output string) string {
	const marker = "sent: tx "
	index := strings.Index(output, marker)
	if index < 0 {
		return ""
	}
	value := output[index+len(marker):]
	if end := strings.IndexAny(value, " \r\n\t"); end >= 0 {
		value = value[:end]
	}
	if strings.HasPrefix(value, "0x") && len(value) == 66 {
		if raw, err := hex.DecodeString(value[2:]); err == nil && len(raw) == common.HashLength {
			return strings.ToLower(value)
		}
	}
	return ""
}

func claimPreparedTx(output string) (string, string) {
	const marker = "prepared: tx "
	index := strings.Index(output, marker)
	if index < 0 {
		return "", ""
	}
	line := output[index+len(marker):]
	if end := strings.IndexAny(line, "\r\n"); end >= 0 {
		line = line[:end]
	}
	parts := strings.Split(line, " raw ")
	if len(parts) != 2 {
		return "", ""
	}
	hash := strings.ToLower(strings.TrimSpace(parts[0]))
	rawHex := strings.ToLower(strings.TrimSpace(parts[1]))
	if claimTxHash("sent: tx "+hash) == "" || !strings.HasPrefix(rawHex, "0x") {
		return "", ""
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(rawHex, "0x"))
	if err != nil || len(raw) == 0 {
		return "", ""
	}
	var tx types.Transaction
	if tx.UnmarshalBinary(raw) != nil || !strings.EqualFold(tx.Hash().Hex(), hash) {
		return "", ""
	}
	return hash, rawHex
}

type boundedClaimOutput struct {
	mu           sync.Mutex
	buf          []byte
	onTxHash     func(string)
	onPrepared   func(string, string) error
	seenHash     string
	seenPrepared string
}

func (w *boundedClaimOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > 64<<10 {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-(64<<10):]...)
	}
	hash := claimTxHash(string(w.buf))
	preparedHash, preparedRaw := claimPreparedTx(string(w.buf))
	callback := w.onTxHash
	preparedCallback := w.onPrepared
	if hash != "" && hash != w.seenHash {
		w.seenHash = hash
	} else {
		hash = ""
	}
	if preparedHash != "" && preparedHash != w.seenPrepared {
		w.seenPrepared = preparedHash
	} else {
		preparedHash, preparedRaw = "", ""
	}
	w.mu.Unlock()
	if preparedHash != "" && preparedCallback != nil {
		if err := preparedCallback(preparedHash, preparedRaw); err != nil {
			return 0, err
		}
	}
	if hash != "" && callback != nil {
		callback(hash)
	}
	return len(p), nil
}

func (w *boundedClaimOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(append([]byte(nil), w.buf...))
}

func executeClaim(ctx context.Context, executable string, cfg *ClaimDaemonConfig, entry *ClaimQueueEntry, onPrepared func(string, string) error, onTxHash func(string)) (string, error) {
	args := []string{"claim", fmt.Sprintf("--epoch=%d", entry.Epoch), "--api_url=" + cfg.APIURL, "--key_file=" + cfg.KeyFile}
	for _, endpoint := range cfg.RPC {
		args = append(args, "--rpc="+endpoint)
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	output := &boundedClaimOutput{onTxHash: onTxHash, onPrepared: onPrepared}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Run()
	text := output.String()
	return text, err
}

func claimKey(noID []byte, coldkey []byte) ([32]byte, error) {
	var out [32]byte
	if len(noID) == 0 || len(noID) > 32 || len(coldkey) != 32 {
		return out, fmt.Errorf("invalid claim identity: no_id=%d bytes coldkey=%d bytes", len(noID), len(coldkey))
	}
	encoded := make([]byte, 64)
	copy(encoded[32-len(noID):32], noID)
	copy(encoded[32:], coldkey)
	copy(out[:], crypto.Keccak256(encoded))
	return out, nil
}

func claimCalldata(claim *sdk.SnPoolClaimResult) (common.Address, []byte, error) {
	if claim == nil || claim.Epoch < 0 || len(claim.NoId) == 0 || len(claim.NoId) > 32 || len(claim.Coldkey) != 32 || len(claim.PayoutRoot) != 32 || claim.ShareBps <= 0 || claim.ShareBps > 10_000 {
		return common.Address{}, nil, errors.New("claim response has malformed identity, epoch, root, or share")
	}
	vault := claim.SettlementVaultAddress
	if vault == "" {
		vault = claim.ContractAddress
	}
	if !common.IsHexAddress(vault) || (claim.ContractAddress != "" && !strings.EqualFold(vault, claim.ContractAddress)) {
		return common.Address{}, nil, errors.New("claim response has inconsistent settlement vault address")
	}
	noID := new(big.Int).SetBytes(claim.NoId)
	var coldkey, root [32]byte
	copy(coldkey[:], claim.Coldkey)
	copy(root[:], claim.PayoutRoot)
	proof := make([][32]byte, len(claim.Proof))
	for index, node := range claim.Proof {
		if len(node) != 32 {
			return common.Address{}, nil, fmt.Errorf("claim proof node %d has %d bytes", index, len(node))
		}
		copy(proof[index][:], node)
	}
	share := new(big.Int).SetUint64(uint64(claim.ShareBps))
	if !merkle.Verify(root, merkle.PayoutLeaf(coldkey, share), proof) {
		return common.Address{}, nil, errors.New("claim proof does not reconstruct the advertised payout root")
	}
	calldata, err := onchain.BuildClaimCalldata(onchain.ClaimIntent{E: big.NewInt(claim.Epoch), NoID: noID, Coldkey: coldkey, ShareBps: share, Proof: proof})
	if err != nil {
		return common.Address{}, nil, err
	}
	return common.HexToAddress(vault), calldata, nil
}

func finalizedNumber(ctx context.Context, client *ethclient.Client) (uint64, error) {
	var hash string
	if err := client.Client().CallContext(ctx, &hash, "chain_getFinalizedHead"); err != nil {
		return 0, err
	}
	var header struct {
		Number string `json:"number"`
	}
	if err := client.Client().CallContext(ctx, &header, "chain_getHeader", hash); err != nil {
		return 0, err
	}
	return parseEthHexQuantity(header.Number)
}

func queryClaimedFinalized(ctx context.Context, cfg *ClaimDaemonConfig, claim *sdk.SnPoolClaimResult) (bool, error) {
	if claim == nil {
		return false, errors.New("claim response is nil")
	}
	if claim.Error != nil {
		return false, errors.New(claim.Error.Message)
	}
	if claim.ChainId < 0 {
		return false, fmt.Errorf("negative claim chain id %d", claim.ChainId)
	}
	vault := claim.SettlementVaultAddress
	if vault == "" {
		vault = claim.ContractAddress
	}
	if !common.IsHexAddress(vault) {
		return false, fmt.Errorf("invalid settlement vault address %q", vault)
	}
	if claim.ContractAddress != "" && !strings.EqualFold(vault, claim.ContractAddress) {
		return false, fmt.Errorf("claim contract %s disagrees with settlement vault %s", claim.ContractAddress, vault)
	}
	key, err := claimKey(claim.NoId, claim.Coldkey)
	if err != nil {
		return false, err
	}
	epoch := big.NewInt(claim.Epoch)
	noID := new(big.Int).SetBytes(claim.NoId)
	data := stSettlementVault.PackLeafClaimed(epoch, key)
	var advertisedRoot [32]byte
	if len(claim.PayoutRoot) != 32 {
		return false, errors.New("claim response has no 32-byte payout root")
	}
	copy(advertisedRoot[:], claim.PayoutRoot)
	var failures []error
	for _, endpoint := range cfg.RPC {
		client, dialErr := ethclient.DialContext(ctx, endpoint)
		if dialErr != nil {
			failures = append(failures, fmt.Errorf("%s: %w", endpoint, dialErr))
			continue
		}
		chainID, idErr := client.ChainID(ctx)
		if idErr != nil || chainID == nil || !chainID.IsUint64() || chainID.Uint64() != uint64(claim.ChainId) {
			client.Close()
			failures = append(failures, fmt.Errorf("%s: chain id %v, want %d: %v", endpoint, chainID, claim.ChainId, idErr))
			continue
		}
		finalized, finalErr := finalizedNumber(ctx, client)
		if finalErr != nil {
			client.Close()
			failures = append(failures, fmt.Errorf("%s: finalized head: %w", endpoint, finalErr))
			continue
		}
		address := common.HexToAddress(vault)
		entitlementData := stSettlementVault.PackEntitlement(epoch, noID)
		entitlementOut, entitlementErr := client.CallContract(ctx, ethereum.CallMsg{To: &address, Data: entitlementData}, new(big.Int).SetUint64(finalized))
		if entitlementErr != nil {
			client.Close()
			failures = append(failures, fmt.Errorf("%s: entitlement: %w", endpoint, entitlementErr))
			continue
		}
		entitlement, entitlementErr := stSettlementVault.UnpackEntitlement(entitlementOut)
		if entitlementErr != nil {
			client.Close()
			failures = append(failures, fmt.Errorf("%s: decode finalized entitlement: %w", endpoint, entitlementErr))
			continue
		}
		if entitlement.PayoutRoot != advertisedRoot {
			client.Close()
			failures = append(failures, &claimArtifactRootMismatchError{epoch: claim.Epoch, advertised: advertisedRoot, finalized: entitlement.PayoutRoot})
			continue
		}
		out, callErr := client.CallContract(ctx, ethereum.CallMsg{To: &address, Data: data}, new(big.Int).SetUint64(finalized))
		client.Close()
		if callErr != nil {
			failures = append(failures, fmt.Errorf("%s: leafClaimed: %w", endpoint, callErr))
			continue
		}
		claimed, unpackErr := stSettlementVault.UnpackLeafClaimed(out)
		if unpackErr != nil {
			failures = append(failures, fmt.Errorf("%s: decode leafClaimed: %w", endpoint, unpackErr))
			continue
		}
		return claimed, nil
	}
	return false, errors.Join(failures...)
}

// The caller already authenticated the exact signed transaction. Replaying
// those bytes needs finalized chain authority, not a retained historical API.
func rebroadcastSignedClaim(ctx context.Context, cfg *ClaimDaemonConfig, tx *types.Transaction, from common.Address) (bool, error) {
	var failures []error
	for _, endpoint := range cfg.RPC {
		client, dialErr := ethclient.DialContext(ctx, endpoint)
		if dialErr != nil {
			failures = append(failures, dialErr)
			continue
		}
		chainID, idErr := client.ChainID(ctx)
		if idErr != nil || chainID == nil || chainID.Cmp(tx.ChainId()) != 0 {
			client.Close()
			failures = append(failures, fmt.Errorf("%s chain identity mismatch", endpoint))
			continue
		}
		finalized, finalErr := finalizedNumber(ctx, client)
		if finalErr != nil {
			client.Close()
			failures = append(failures, finalErr)
			continue
		}
		nonce, nonceErr := client.NonceAt(ctx, from, new(big.Int).SetUint64(finalized))
		if nonceErr != nil {
			client.Close()
			failures = append(failures, nonceErr)
			continue
		}
		if nonce > tx.Nonce() {
			client.Close()
			return true, nil
		}
		// The vault checks the saved epoch, proof and claim status at the
		// finalized head. A now-invalid intent remains a signed liability;
		// it is never converted into a new transaction or erased.
		_, preflightErr := client.CallContract(ctx, ethereum.CallMsg{From: from, To: tx.To(), Gas: tx.Gas(), GasPrice: tx.GasPrice(), Value: tx.Value(), Data: tx.Data()}, new(big.Int).SetUint64(finalized))
		if preflightErr != nil {
			client.Close()
			failures = append(failures, fmt.Errorf("finalized exact-claim preflight: %w", preflightErr))
			continue
		}
		sendErr := client.SendTransaction(ctx, tx)
		client.Close()
		if sendErr == nil || knownClaimTransaction(sendErr) {
			return false, nil
		}
		failures = append(failures, sendErr)
	}
	if len(failures) == 0 {
		return false, errors.New("exact claim replay has no RPC endpoint")
	}
	return false, errors.Join(failures...)
}

func knownClaimTransaction(err error) bool {
	if err == nil {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "known transaction") || strings.Contains(message, "already known") || strings.Contains(message, "already imported") || strings.Contains(message, "nonce too low") || strings.Contains(message, "replacement transaction underpriced")
}

// Claim discovery needs only the epoch clock. Optional chain settings in the
// shared API response have independent wire types and are not claim authority.
func readClaimEpoch(ctx context.Context, strategy *connect.ClientStrategy, apiURL, byJWT string) (int64, error) {
	body, err := connect.HttpGetWithStrategyRaw(ctx, strategy, apiURL+"/sn/epoch", byJWT)
	if err != nil {
		return 0, err
	}
	var summary struct {
		Epoch *int64 `json:"epoch"`
	}
	if err := json.Unmarshal(body, &summary); err != nil {
		return 0, err
	}
	if summary.Epoch == nil || *summary.Epoch < 0 {
		return 0, errors.New("claim epoch response has no nonnegative epoch")
	}
	return *summary.Epoch, nil
}

type claimAPI interface {
	SnPoolClaimSyncWithContext(context.Context, *sdk.SnPoolClaimArgs) (*sdk.SnPoolClaimResult, error)
}

// The daemon calls reconciliation and submission inside one shared admission.
// Its finite context also bounds every multi-call finalized observation.
func reconcileClaimEntry(ctx context.Context, cfg *ClaimDaemonConfig, api claimAPI, entry *ClaimQueueEntry) (string, error) {
	if entry == nil || cfg == nil {
		return "", errors.New("claim reconciliation is unavailable")
	}
	if entry.TxHash != "" || entry.RawTxHex != "" {
		return reconcileSignedClaim(ctx, cfg, entry)
	}
	if api == nil {
		return "", errors.New("claim API is unavailable")
	}
	claim, err := api.SnPoolClaimSyncWithContext(ctx, &sdk.SnPoolClaimArgs{Epoch: entry.Epoch})
	if err != nil {
		return "", err
	}
	if claim == nil {
		return "", errors.New("claim response is nil")
	}
	if claim.Error != nil {
		return "", errors.New(claim.Error.Message)
	}
	if claim.Epoch != entry.Epoch {
		return "", errors.New("claim artifact epoch differs from the queued epoch")
	}
	if len(claim.NoId) == 0 {
		// Only an unsigned entry can become a terminal API zero payout.
		// Signed liabilities need exact canonical receipt evidence instead.
		return "no-claim", nil
	}
	claimed, err := queryClaimedFinalized(ctx, cfg, claim)
	if err != nil {
		return "", err
	}
	if claimed {
		return "finalized", nil
	}
	return "", nil
}

// Admission already covers reconciliation through finality. Never wait for
// another nonce lock after recording the durable submitting state.
func submitClaimDirect(ctx context.Context, cfg *ClaimDaemonConfig, api claimAPI, entry *ClaimQueueEntry, store *claimQueueStore, queue *ClaimQueue, admission *claimAdmission) error {
	claim, err := api.SnPoolClaimSyncWithContext(ctx, &sdk.SnPoolClaimArgs{Epoch: entry.Epoch})
	if err != nil {
		return err
	}
	if claim == nil {
		return errors.New("claim response is nil")
	}
	if claim.Error != nil {
		return errors.New(claim.Error.Message)
	}
	if claim.Epoch != entry.Epoch {
		return errors.New("claim artifact epoch differs from the queued epoch")
	}
	vault, calldata, err := claimCalldata(claim)
	if err != nil {
		return err
	}
	if claim.ChainId < 0 {
		return fmt.Errorf("negative claim chain id %d", claim.ChainId)
	}
	key, err := onchain.LoadKeyFile(cfg.KeyFile)
	if err != nil {
		return err
	}
	if err := admission.checkChain(big.NewInt(claim.ChainId)); err != nil {
		return err
	}
	receipt, err := onchain.SubmitWithHooks(ctx, onchain.SubmitParams{
		Contract: vault, Rpcs: cfg.RPC, Key: key, Calldata: calldata, NonceFloor: admission.nonceMinimum(),
		ChainID: new(big.Int).SetUint64(uint64(claim.ChainId)),
	}, onchain.SubmitHooks{
		Prepared: func(hash common.Hash, raw []byte) error {
			priorHash, priorRaw := entry.TxHash, entry.RawTxHex
			entry.TxHash = strings.ToLower(hash.Hex())
			entry.RawTxHex = "0x" + hex.EncodeToString(raw)
			entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			if saveErr := store.save(queue); saveErr != nil {
				entry.TxHash, entry.RawTxHex = priorHash, priorRaw
				return saveErr
			}
			return admission.rememberSigned(cfg, entry)
		},
		Broadcast: func(hash common.Hash) error {
			if !strings.EqualFold(entry.TxHash, hash.Hex()) {
				return errors.New("broadcast transaction hash differs from the durable prepared hash")
			}
			entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return store.save(queue)
		},
	})
	if err != nil {
		return err
	}
	tx, intent, from, err := authenticateSignedClaim(cfg, entry)
	if err != nil {
		return err
	}
	if err := verifySignedClaimReceipt(tx, intent, from, receipt); err != nil {
		return err
	}
	return recordFinalizedClaimReceipt(entry, receipt)
}

// recordFinalizedClaimReceipt binds a successful queue entry to the exact
// canonical receipt which caused it. Raw signed transaction bytes alone prove
// intent but not inclusion, so the simulator's immutable evidence capture
// retains the finalized block and a canonical hash of every receipt log.
func recordFinalizedClaimReceipt(entry *ClaimQueueEntry, receipt *types.Receipt) error {
	if entry == nil || receipt == nil || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Sign() <= 0 || receipt.BlockHash == (common.Hash{}) || receipt.TxHash == (common.Hash{}) {
		return errors.New("claim returned an incomplete finalized receipt")
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return errors.New("claim finalized with a failed receipt")
	}
	if entry.TxHash == "" || !strings.EqualFold(entry.TxHash, receipt.TxHash.Hex()) {
		return errors.New("claim receipt transaction differs from the durable prepared transaction")
	}
	logs, err := json.Marshal(receipt.Logs)
	if err != nil {
		return fmt.Errorf("encode claim receipt logs: %w", err)
	}
	digest := sha256.Sum256(logs)
	entry.FinalizedBlock = receipt.BlockNumber.Uint64()
	entry.FinalizedBlockHash = strings.ToLower(receipt.BlockHash.Hex())
	entry.ReceiptStatus = receipt.Status
	entry.ReceiptLogsHash = "sha256:" + hex.EncodeToString(digest[:])
	return nil
}

// finalizedClaimReceipt recovers inclusion evidence after a crash between
// chain finality and the queue fsync. An endpoint must provide a canonical
// finalized block before its recovered receipt is accepted.
func finalizedClaimReceipt(ctx context.Context, cfg *ClaimDaemonConfig, txHash string, chainId *big.Int) (*types.Receipt, error) {
	if cfg == nil || chainId == nil || chainId.Sign() <= 0 {
		return nil, errors.New("claim receipt chain configuration is unavailable")
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(txHash, "0x"))
	if err != nil || len(raw) != common.HashLength {
		return nil, fmt.Errorf("invalid claim transaction hash %q", txHash)
	}
	hash := common.BytesToHash(raw)
	var failures []error
	for _, endpoint := range cfg.RPC {
		client, dialErr := ethclient.DialContext(ctx, endpoint)
		if dialErr != nil {
			failures = append(failures, dialErr)
			continue
		}
		observedChainId, idErr := client.ChainID(ctx)
		if idErr != nil || observedChainId == nil || observedChainId.Cmp(chainId) != 0 {
			client.Close()
			failures = append(failures, fmt.Errorf("claim receipt RPC chain identity mismatch: %v", idErr))
			continue
		}
		receipt, receiptErr := client.TransactionReceipt(ctx, hash)
		if receiptErr != nil {
			client.Close()
			failures = append(failures, receiptErr)
			continue
		}
		if receipt == nil || receipt.TxHash != hash || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Sign() <= 0 || receipt.BlockHash == (common.Hash{}) {
			client.Close()
			failures = append(failures, errors.New("claim receipt has an incomplete or mismatched identity"))
			continue
		}
		finalized, finalErr := finalizedNumber(ctx, client)
		if finalErr != nil || finalized < receipt.BlockNumber.Uint64() {
			client.Close()
			if finalErr != nil {
				failures = append(failures, finalErr)
			} else {
				failures = append(failures, errors.New("claim receipt is not finalized"))
			}
			continue
		}
		block, blockErr := onchain.ReadEVMBlockIdentity(ctx, client, receipt.BlockNumber)
		client.Close()
		if blockErr != nil {
			failures = append(failures, blockErr)
			continue
		}
		if block.Hash != receipt.BlockHash {
			return nil, fmt.Errorf("claim transaction %s receipt is not canonical", txHash)
		}
		return receipt, nil
	}
	if len(failures) == 0 {
		return nil, errors.New("claim receipt has no RPC endpoint")
	}
	return nil, errors.Join(failures...)
}

func readClaimDaemonJWT(cfg *ClaimDaemonConfig) (string, error) {
	if cfg.JWTFile == "" {
		return readNetworkJwt()
	}
	b, err := os.ReadFile(cfg.JWTFile)
	if err != nil {
		return "", err
	}
	jwt := strings.TrimSpace(string(b))
	if jwt == "" {
		return "", errors.New("claim network JWT is empty")
	}
	return jwt, nil
}

func runClaimDaemonWithAdmission(ctx context.Context, configPath string, admission *claimAdmission, initialDelay time.Duration, onReady func()) (runErr error) {
	if ctx == nil || admission == nil {
		return errors.New("claim daemon context or admission is unavailable")
	}
	cfg, err := LoadClaimDaemonConfig(configPath)
	if err != nil {
		return err
	}
	defer admission.forget(cfg.StateDir)
	store, err := newClaimQueueStore(cfg.StateDir)
	if err != nil {
		return err
	}
	if err := admission.seedMember(cfg); err != nil {
		return err
	}
	queue, err := store.load()
	if err != nil {
		return err
	}
	if err := store.save(queue); err != nil {
		return err
	}
	strategy := connect.NewClientStrategyWithDefaults(ctx)
	defer strategy.Close()
	api := sdk.NewApi(ctx, strategy, cfg.APIURL)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), claimOperationTimeout)
		defer closeCancel()
		runErr = errors.Join(runErr, api.CloseAndWait(closeCtx))
	}()
	jwt, err := readClaimDaemonJWT(cfg)
	if err != nil {
		return err
	}
	api.SetByJwt(jwt)
	if onReady != nil {
		onReady()
	}
	if initialDelay > 0 {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(initialDelay):
		}
	}
	period := time.Duration(cfg.PollSeconds) * time.Second
	epochCtx, epochCancel := context.WithCancel(ctx)
	epochs, epochDone := startClaimEpochReader(epochCtx, period, func(ctx context.Context) (int64, error) {
		return readClaimEpoch(ctx, strategy, cfg.APIURL, api.GetByJwt())
	})
	defer func() { epochCancel(); <-epochDone }()
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	return runClaimQueue(ctx, queue, cfg.LookbackEpochs, epochs, ticker.C, func() <-chan struct{} {
		return admission.ready(cfg.StateDir)
	}, claimQueuePollHooks{
		now: time.Now, save: store.save, latestEpoch: admission.observeEpoch,
		begin: func(ctx context.Context, candidates []claimPollCandidate) (int, context.Context, func(), error) {
			return beginClaimOperation(ctx, admission, cfg.StateDir, candidates)
		},
		reconcile: func(ctx context.Context, entry *ClaimQueueEntry) (string, error) {
			return reconcileClaimEntry(ctx, cfg, api, entry)
		},
		submit: func(ctx context.Context, entry *ClaimQueueEntry) error {
			return submitClaimDirect(ctx, cfg, api, entry, store, queue, admission)
		},
	})
}

// RunClaimDaemon runs the production claim worker using direct package calls;
// it never shells out to the provider or snclaim CLIs.
func RunClaimDaemon(ctx context.Context, configPath string) error {
	return runClaimDaemonWithAdmission(ctx, configPath, &claimAdmission{}, 0, nil)
}

func runClaimDaemon(configPath string) error {
	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	return RunClaimDaemon(event.Ctx(), configPath)
}
