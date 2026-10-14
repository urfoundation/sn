package onchain

// v2.go — `snclaim v2` (VERSION2-FINAL.md §10.2): status, claim and check
// against the immutable vault2 (STSettlementVault2) and the operator's
// public ledger endpoints, GET /sn/ledger/account and GET
// /sn/ledger/object. Amounts are rao. Nothing the endpoints serve is
// trusted: a claim's proof is verified against the root vault2 holds, and
// check rebuilds the pending root from the objects it fetches.

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/holiman/uint256"

	docopt "github.com/docopt/docopt-go"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/stabi"
)

// vault2 packs and decodes STSettlementVault2 calls and events.
var vault2 = stabi.NewSTSettlementVault2()

// parsedVault2ABI parses vault2's ABI once, for its error and event IDs.
var parsedVault2ABI = sync.OnceValues(func() (*abi.ABI, error) {
	return stabi.STSettlementVault2MetaData.ParseABI()
})

// v2Random is the randomness of the demand sample; tests replace it.
var v2Random io.Reader = rand.Reader

// v2HTTP reads the ledger endpoints; an object of several MiB has minutes.
var v2HTTP = &http.Client{Timeout: 5 * time.Minute}

// v2AccountReplyLimit bounds GET /sn/ledger/account: one entry per pool.
const v2AccountReplyLimit = 1 << 20

// cmdV2 dispatches `snclaim v2 status|claim|check`.
func cmdV2(ctx context.Context, opts docopt.Opts) error {
	switch {
	case boolOpt(opts, "claim"):
		return cmdV2Claim(ctx, opts)
	case boolOpt(opts, "check"):
		return cmdV2Check(ctx, opts)
	default:
		return cmdV2Status(ctx, opts)
	}
}

// parseVault2 parses --vault as vault2's address.
func parseVault2(s string) (common.Address, error) {
	if !common.IsHexAddress(s) {
		return common.Address{}, fmt.Errorf("--vault: %q is not a 20-byte hex address", s)
	}
	return common.HexToAddress(s), nil
}

// parseAPIURL parses --api_url, the operator API serving the ledger.
func parseAPIURL(s string) (*url.URL, error) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("--api_url: %q is not an http(s) URL without a query", s)
	}
	if u.Path == "" {
		u.Path = "/" // so that JoinPath gives an absolute path
	}
	return u, nil
}

// parseUint64Opt parses an optional non-negative integer flag; ok is false
// when it is absent.
func parseUint64Opt(opts docopt.Opts, flag string) (v uint64, ok bool, err error) {
	if !hasOpt(opts, flag) {
		return 0, false, nil
	}
	s := strOpt(opts, flag)
	if v, err = strconv.ParseUint(s, 0, 64); err != nil {
		return 0, false, fmt.Errorf("%s: %q is not an unsigned 64-bit integer", flag, s)
	}
	return v, true, nil
}

// v2Connect dials the first reachable --rpc and checks --chain_id.
func v2Connect(ctx context.Context, opts docopt.Opts, wantChainID *big.Int) (*ethclient.Client, *big.Int, string, error) {
	client, chainID, rpcURL, err := dialFirst(ctx, strsOpt(opts, "--rpc"))
	if err != nil {
		return nil, nil, "", err
	}
	if wantChainID != nil && chainID.Cmp(wantChainID) != 0 {
		client.Close()
		return nil, nil, "", fmt.Errorf("chain id mismatch: --chain_id=%s but %s reports %s", wantChainID, rpcURL, chainID)
	}
	return client, chainID, rpcURL, nil
}

// v2Reader reads vault2's views at one block, nil for the latest.
type v2Reader struct {
	client *ethclient.Client
	vault  common.Address
	block  *big.Int
}

// v2View calls a vault2 view and decodes its answer.
func v2View[T any](ctx context.Context, r *v2Reader, data []byte, unpack func([]byte) (T, error)) (T, error) {
	ret, err := ethCallAt(ctx, r.client, r.vault, data, r.block)
	if err != nil {
		var zero T
		return zero, err
	}
	return unpack(ret)
}

// v2Reverted reports a transaction refused by its preflight or estimate
// with revert data, or mined and reverted. Senders then read chain state to
// tell a third party's front-run, which is success (SPEC A6), from a
// failure.
func v2Reverted(err error) bool {
	return onchainRevertData(err) != nil || errors.Is(err, errRevertedOnChain)
}

// v2Sender signs and sends vault2 transactions through runTx.
type v2Sender struct {
	client   *ethclient.Client
	chainID  *big.Int
	rpcURL   string
	vault    common.Address
	from     common.Address
	key      *ecdsa.PrivateKey
	gasLimit uint64
	dryRun   bool
}

// send runs one transaction, its estimated gas limit at least gasFloor;
// describe prints its intent lines.
func (s *v2Sender) send(ctx context.Context, what string, calldata []byte, value *big.Int, gasFloor uint64, describe func()) (*types.Receipt, error) {
	return runTx(ctx, s.client, s.chainID, txRequest{
		contract: s.vault,
		from:     s.from,
		key:      s.key,
		calldata: calldata,
		value:    value,
		gasLimit: s.gasLimit,
		gasFloor: gasFloor,
		dryRun:   s.dryRun,
	}, func(gasEst uint64, gasErr error) {
		fmt.Printf("settlement v2 %s intent\n", what)
		fmt.Printf("  vault:       %s (chain id %s, rpc %s)\n", s.vault.Hex(), s.chainID, s.rpcURL)
		fmt.Printf("  from:        %s\n", s.from.Hex())
		describe()
		fmt.Printf("  calldata:    %d bytes, selector 0x%x\n", len(calldata), calldata[:4])
		if gasErr == nil {
			fmt.Printf("  gas (est):   %d\n", gasEst)
		} else {
			fmt.Printf("  gas (est):   unavailable (%v); using --gas_limit=%d\n", gasErr, s.gasLimit)
		}
	})
}

// v2Account is one pool's entry of GET /sn/ledger/account.
type v2Account struct {
	Vault      string  `json:"vault"`
	NoID       uint64  `json:"no_id"`
	Index      uint64  `json:"index"`
	RootIndex  *uint64 `json:"root_index"`
	Cumulative string  `json:"cumulative"`
	Proof      []struct {
		Hash string `json:"hash"`
		Sum  string `json:"sum"`
	} `json:"proof"`
	PendingCumulative *string `json:"pending_cumulative"`
}

// v2Get opens the body of a 200 answer to GET u. The request is built
// from the parsed URL, which leaves nothing to refuse.
func v2Get(ctx context.Context, u *url.URL) (io.ReadCloser, error) {
	response, err := v2HTTP.Do((&http.Request{Method: http.MethodGet, URL: u, Header: http.Header{}}).WithContext(ctx))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", u, response.Status)
	}
	return response.Body, nil
}

// fetchV2Accounts reads the coldkey's accounts from GET /sn/ledger/account,
// within the time of a chain call.
func fetchV2Accounts(ctx context.Context, api *url.URL, coldkey [32]byte) ([]v2Account, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	u := api.JoinPath("sn", "ledger", "account")
	u.RawQuery = url.Values{"coldkey": {fmt.Sprintf("0x%x", coldkey)}}.Encode()
	body, err := v2Get(ctx, u)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var reply struct {
		Coldkey  string      `json:"coldkey"`
		Accounts []v2Account `json:"accounts"`
	}
	if err := json.NewDecoder(io.LimitReader(body, v2AccountReplyLimit)).Decode(&reply); err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	if reply.Coldkey != fmt.Sprintf("0x%x", coldkey) || reply.Accounts == nil {
		return nil, fmt.Errorf("GET %s answered for coldkey %q with accounts %v", u, reply.Coldkey, reply.Accounts)
	}
	return reply.Accounts, nil
}

// check refuses an entry of another vault than --vault.
func (a *v2Account) check(vault common.Address) error {
	if !common.IsHexAddress(a.Vault) || common.HexToAddress(a.Vault) != vault {
		return fmt.Errorf("the ledger endpoint lists pool %d in vault %q, not --vault %s", a.NoID, a.Vault, vault.Hex())
	}
	return nil
}

// v2Amount parses a decimal rao amount of the account endpoint.
func v2Amount(field, s string) (*uint256.Int, error) {
	v, err := uint256.FromDecimal(s)
	if err != nil {
		return nil, fmt.Errorf("ledger account %s %q: %w", field, s, err)
	}
	return v, nil
}

// proof parses the entry's bottom-up proof.
func (a *v2Account) proof() ([]merkle.LedgerNode, error) {
	nodes := make([]merkle.LedgerNode, len(a.Proof))
	for k, node := range a.Proof {
		hash, err := hexutil.Decode(node.Hash)
		if err != nil || len(hash) != 32 {
			return nil, fmt.Errorf("ledger account proof node %d hash %q is not 32 bytes of 0x hex", k, node.Hash)
		}
		sum, err := v2Amount(fmt.Sprintf("proof node %d sum", k), node.Sum)
		if err != nil {
			return nil, err
		}
		nodes[k] = merkle.LedgerNode{Hash: [32]byte(hash), Sum: *sum}
	}
	return nodes, nil
}

// v2LedgerAccount, v2ProofNodes and v2PathNodes convert an account at
// index, a bottom-up proof and a top-down path to vault2's argument types.
func v2LedgerAccount(index uint64, account *merkle.LedgerAccount) stabi.LedgerTreeAccount {
	return stabi.LedgerTreeAccount{Index: new(big.Int).SetUint64(index), Coldkey: account.Coldkey, Cumulative: account.Cumulative.ToBig()}
}

func v2ProofNodes(nodes []merkle.LedgerNode) []stabi.LedgerTreeProofNode {
	out := make([]stabi.LedgerTreeProofNode, len(nodes))
	for k := range nodes {
		out[k] = stabi.LedgerTreeProofNode{Hash: nodes[k].Hash, Sum: nodes[k].Sum.ToBig()}
	}
	return out
}

func v2PathNodes(nodes []merkle.LedgerPathNode) []stabi.LedgerTreePathNode {
	out := make([]stabi.LedgerTreePathNode, len(nodes))
	for k := range nodes {
		out[k] = stabi.LedgerTreePathNode{LeftHash: nodes[k].LeftHash, LeftSum: nodes[k].LeftSum.ToBig(), RightHash: nodes[k].RightHash, RightSum: nodes[k].RightSum.ToBig()}
	}
	return out
}

// v2Alpha renders a rao amount.
func v2Alpha(v *uint256.Int) string {
	return formatAlpha(v.ToBig())
}
