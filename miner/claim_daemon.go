package miner

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/urnetwork/connect"
	"github.com/urnetwork/sdk"
	"gopkg.in/yaml.v3"

	"github.com/urfoundation/sn/merkle"
	"github.com/urfoundation/sn/miner/onchain"
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
	if err := dec.Decode(&extra); err == nil {
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
	Epoch       int64  `json:"epoch"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	UpdatedAt   string `json:"updated_at"`
	NextRetryAt string `json:"next_retry_at,omitempty"`
	TxHash      string `json:"tx_hash,omitempty"`
	RawTxHex    string `json:"raw_tx_hex,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

type ClaimQueue struct {
	Schema         string                      `json:"schema"`
	LastDiscovered int64                       `json:"last_discovered"`
	Entries        map[string]*ClaimQueueEntry `json:"entries"`
}

type claimQueueStore struct{ path string }

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
		if entry.Status == "submitting" {
			if entry.RawTxHex != "" && entry.TxHash != "" {
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

func (s *claimQueueStore) save(q *ClaimQueue) error {
	b, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".claim-queue-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
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
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
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
		if idErr != nil || !chainID.IsUint64() || chainID.Uint64() != uint64(claim.ChainId) {
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
		if entitlementErr != nil || entitlement.PayoutRoot != advertisedRoot {
			client.Close()
			failures = append(failures, fmt.Errorf("%s: finalized payout root does not match claim artifact", endpoint))
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

// uncertainClaimRetryable returns true only after the exact transaction has
// a canonical, finalized failure receipt. A missing or pending transaction is
// intentionally left uncertain; leafClaimed is checked first on every pass.
func uncertainClaimRetryable(ctx context.Context, cfg *ClaimDaemonConfig, txHash string) (bool, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(txHash, "0x"))
	if err != nil || len(raw) != common.HashLength {
		return false, fmt.Errorf("invalid uncertain transaction hash %q", txHash)
	}
	hash := common.BytesToHash(raw)
	var failures []error
	for _, endpoint := range cfg.RPC {
		client, dialErr := ethclient.DialContext(ctx, endpoint)
		if dialErr != nil {
			failures = append(failures, dialErr)
			continue
		}
		receipt, receiptErr := client.TransactionReceipt(ctx, hash)
		if receiptErr == ethereum.NotFound {
			client.Close()
			continue
		}
		if receiptErr != nil {
			client.Close()
			failures = append(failures, receiptErr)
			continue
		}
		finalized, finalErr := finalizedNumber(ctx, client)
		if finalErr != nil || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || finalized < receipt.BlockNumber.Uint64() {
			client.Close()
			if finalErr != nil {
				failures = append(failures, finalErr)
			}
			continue
		}
		header, headerErr := client.HeaderByNumber(ctx, receipt.BlockNumber)
		client.Close()
		if headerErr != nil {
			failures = append(failures, headerErr)
			continue
		}
		if header.Hash() != receipt.BlockHash {
			return false, fmt.Errorf("transaction %s receipt is not canonical", txHash)
		}
		if receipt.Status == types.ReceiptStatusFailed {
			return true, nil
		}
		return false, fmt.Errorf("transaction %s finalized successfully but leafClaimed is false", txHash)
	}
	if len(failures) > 0 {
		return false, errors.Join(failures...)
	}
	return false, nil
}

func rebroadcastExactClaim(ctx context.Context, cfg *ClaimDaemonConfig, claim *sdk.SnPoolClaimResult, entry *ClaimQueueEntry) (bool, error) {
	vault, calldata, err := claimCalldata(claim)
	if err != nil {
		return false, err
	}
	raw, err := hex.DecodeString(strings.TrimPrefix(entry.RawTxHex, "0x"))
	if err != nil || len(raw) == 0 {
		return false, errors.New("uncertain claim has no valid exact signed transaction")
	}
	var tx types.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil || !strings.EqualFold(tx.Hash().Hex(), entry.TxHash) {
		return false, errors.New("uncertain claim transaction bytes/hash mismatch")
	}
	if tx.To() == nil || *tx.To() != vault || tx.Value().Sign() != 0 || !bytes.Equal(tx.Data(), calldata) {
		return false, errors.New("uncertain claim transaction does not match the current verified claim")
	}
	if claim.ChainId < 0 || !tx.ChainId().IsUint64() || tx.ChainId().Uint64() != uint64(claim.ChainId) {
		return false, errors.New("uncertain claim transaction chain identity mismatch")
	}
	signer := types.LatestSignerForChainID(tx.ChainId())
	from, err := types.Sender(signer, &tx)
	if err != nil {
		return false, err
	}
	key, err := onchain.LoadKeyFile(cfg.KeyFile)
	if err != nil || crypto.PubkeyToAddress(key.PublicKey) != from {
		return false, errors.New("uncertain claim signer does not match configured relayer")
	}
	var failures []error
	for _, endpoint := range cfg.RPC {
		client, dialErr := ethclient.DialContext(ctx, endpoint)
		if dialErr != nil {
			failures = append(failures, dialErr)
			continue
		}
		chainID, idErr := client.ChainID(ctx)
		if idErr != nil || chainID.Cmp(tx.ChainId()) != 0 {
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
		sendErr := client.SendTransaction(ctx, &tx)
		client.Close()
		if sendErr == nil || knownClaimTransaction(sendErr) {
			return false, nil
		}
		failures = append(failures, sendErr)
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

type claimAPI interface {
	SnPoolClaimSyncWithContext(context.Context, *sdk.SnPoolClaimArgs) (*sdk.SnPoolClaimResult, error)
}

// The injectable form used to prove that finalized observation shares the
// operator-local chain/nonce boundary with transaction submission.
type claimReconcileFunc func(context.Context, *ClaimDaemonConfig, claimAPI, *ClaimQueueEntry) (string, error)

// Serializes multi-call finalized reconciliation for one operator. Without
// this boundary, hundreds of payable miners can enter a source-wide public RPC
// queue at once and starve validators even though submissions are serialized.
func reconcileClaimEntryWithLock(ctx context.Context, cfg *ClaimDaemonConfig, api claimAPI, entry *ClaimQueueEntry, chainStateLock *sync.Mutex, reconcile claimReconcileFunc) (string, error) {
	if chainStateLock == nil || reconcile == nil {
		return "", errors.New("claim reconciliation chain boundary is unavailable")
	}
	chainStateLock.Lock()
	defer chainStateLock.Unlock()
	return reconcile(ctx, cfg, api, entry)
}

func reconcileClaimEntry(ctx context.Context, cfg *ClaimDaemonConfig, api claimAPI, entry *ClaimQueueEntry) (string, error) {
	claim, err := api.SnPoolClaimSyncWithContext(ctx, &sdk.SnPoolClaimArgs{Epoch: entry.Epoch})
	if err != nil {
		return "", err
	}
	if claim.Error != nil {
		return "", errors.New(claim.Error.Message)
	}
	if len(claim.NoId) == 0 {
		// A finalized epoch with no provider leaf is a terminal zero payout, not
		// a transaction failure to retry forever.
		return "no-claim", nil
	}
	claimed, err := queryClaimedFinalized(ctx, cfg, claim)
	if err != nil {
		return "", err
	}
	if claimed {
		return "finalized", nil
	}
	if entry.Status == "uncertain" && entry.TxHash != "" {
		retryable, err := uncertainClaimRetryable(ctx, cfg, entry.TxHash)
		if err != nil {
			return "", err
		}
		if retryable {
			return "retry", nil
		}
		retryable, err = rebroadcastExactClaim(ctx, cfg, claim, entry)
		if err != nil {
			return "", err
		}
		if retryable {
			return "retry", nil
		}
	}
	return "", nil
}

func submitClaimDirect(ctx context.Context, cfg *ClaimDaemonConfig, api claimAPI, entry *ClaimQueueEntry, submitLock *sync.Mutex, store *claimQueueStore, queue *ClaimQueue) error {
	claim, err := api.SnPoolClaimSyncWithContext(ctx, &sdk.SnPoolClaimArgs{Epoch: entry.Epoch})
	if err != nil {
		return err
	}
	if claim.Error != nil {
		return errors.New(claim.Error.Message)
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
	if submitLock == nil {
		return errors.New("claim submit lock is nil")
	}
	submitLock.Lock()
	defer submitLock.Unlock()
	_, err = onchain.SubmitWithHooks(ctx, onchain.SubmitParams{
		Contract: vault, Rpcs: cfg.RPC, Key: key, Calldata: calldata,
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
			return nil
		},
		Broadcast: func(hash common.Hash) error {
			if !strings.EqualFold(entry.TxHash, hash.Hex()) {
				return errors.New("broadcast transaction hash differs from the durable prepared hash")
			}
			entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return store.save(queue)
		},
	})
	return err
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

func runClaimDaemonWithLock(ctx context.Context, configPath string, chainStateLock *sync.Mutex, initialDelay time.Duration, onReady func()) error {
	if ctx == nil {
		return errors.New("claim daemon context is nil")
	}
	cfg, err := LoadClaimDaemonConfig(configPath)
	if err != nil {
		return err
	}
	store, err := newClaimQueueStore(cfg.StateDir)
	if err != nil {
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
		_ = api.CloseAndWait(context.Background())
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
	ticker := time.NewTicker(time.Duration(cfg.PollSeconds) * time.Second)
	defer ticker.Stop()
	for {
		current, err := api.SnEpochSyncWithContext(ctx)
		if err == nil {
			discoverClaims(queue, current.Epoch, cfg.LookbackEpochs)
			if err := store.save(queue); err != nil {
				return err
			}
		}
		for epoch := int64(0); epoch <= queue.LastDiscovered; epoch++ {
			entry := queue.Entries[fmt.Sprint(epoch)]
			if entry == nil || entry.Status == "finalized" || entry.Status == "no-claim" {
				continue
			}
			reconciled, reconcileErr := reconcileClaimEntryWithLock(ctx, cfg, api, entry, chainStateLock, reconcileClaimEntry)
			if reconciled != "" {
				entry.Status = reconciled
				entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
				entry.LastError = ""
				entry.NextRetryAt = ""
				if err := store.save(queue); err != nil {
					return err
				}
				if reconciled == "finalized" || reconciled == "no-claim" {
					continue
				}
			}
			if entry.Status == "uncertain" {
				if reconcileErr != nil {
					entry.LastError = "uncertain transaction reconciliation: " + reconcileErr.Error()
					entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
					_ = store.save(queue)
				}
				continue
			}
			if reconcileErr != nil {
				entry.Status = "retry"
				entry.LastError = "claim is not ready for finalized reconciliation: " + reconcileErr.Error()
				entry.NextRetryAt = time.Now().Add(claimRetry(max(1, entry.Attempts))).UTC().Format(time.RFC3339Nano)
				entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
				if err := store.save(queue); err != nil {
					return err
				}
				continue
			}
			if entry.NextRetryAt != "" {
				when, parseErr := time.Parse(time.RFC3339Nano, entry.NextRetryAt)
				if parseErr == nil && time.Now().Before(when) {
					continue
				}
			}
			entry.Status = "submitting"
			entry.Attempts++
			entry.TxHash = ""
			entry.RawTxHex = ""
			entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			entry.LastError = ""
			if err := store.save(queue); err != nil {
				return err
			}
			claimErr := submitClaimDirect(ctx, cfg, api, entry, chainStateLock, store, queue)
			entry.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			if claimErr == nil {
				entry.Status = "finalized"
				entry.NextRetryAt = ""
			} else if entry.TxHash != "" {
				entry.Status = "uncertain"
				entry.LastError = "transaction was broadcast but finality was not confirmed: " + claimErr.Error()
			} else {
				entry.Status = "retry"
				entry.LastError = claimErr.Error()
				entry.NextRetryAt = time.Now().Add(claimRetry(entry.Attempts)).UTC().Format(time.RFC3339Nano)
			}
			if err := store.save(queue); err != nil {
				return err
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// RunClaimDaemon runs the production claim worker using direct package calls;
// it never shells out to the provider or snclaim CLIs.
func RunClaimDaemon(ctx context.Context, configPath string) error {
	var chainStateLock sync.Mutex
	return runClaimDaemonWithLock(ctx, configPath, &chainStateLock, 0, nil)
}

func runClaimDaemon(configPath string) error {
	event := connect.NewEventWithContext(context.Background())
	event.SetOnSignals(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	return RunClaimDaemon(event.Ctx(), configPath)
}
