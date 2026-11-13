// Package bridge builds the Solana side of the LayerZero OFT bridge for
// canonical TAO: a send of TAO from a Solana wallet to wTAO on Bittensor EVM. It
// also reads LayerZero Scan for the message's delivery.
package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/demand/solana"
)

const (
	// BittensorEID and SolanaEID are the two ends' LayerZero endpoint IDs.
	BittensorEID uint32 = 30374
	SolanaEID    uint32 = 30168

	// UnitRao is the least amount the bridge moves: its shared decimals are 6,
	// so it carries whole micro-TAO.
	UnitRao = 1_000

	// A send used 509,418 compute units on mainnet.
	computeUnitLimit = 700_000
	// The priority fee is about 14,000 lamports at this price.
	computeUnitPriceMicroLamports = 20_000

	maxResponseBytes = 4 << 20
	defaultScanBase  = "https://scan.layerzero-api.com/v1"
)

// WTAO is the wrapped TAO contract on Bittensor EVM and the OFT's other end.
var WTAO = common.HexToAddress("0x134f59e8b8637fd70ae12f263492b1dc73a25d1e")

var sendDiscriminator = func() []byte {
	digest := sha256.Sum256([]byte("global:send"))
	return digest[:8]
}()

// typeThreeOptions is an empty LayerZero options block: the peer's enforced
// options alone set the delivery gas.
var typeThreeOptions = []byte{0x00, 0x03}

// Send is one send of TAO to an EVM address on Bittensor.
type Send struct {
	Sender solana.PublicKey
	To     common.Address
	// AmountRao moves as whole units; the send refuses dust instead of losing it.
	AmountRao uint64
	// MaxFeeLamports caps LayerZero's fee. Only the actual fee is charged.
	MaxFeeLamports uint64
}

// Validate reports whether the send can be encoded.
func (s Send) Validate() error {
	switch {
	case s.Sender.IsZero():
		return errors.New("bridge: send needs a sender")
	case s.To == (common.Address{}):
		return errors.New("bridge: send needs a recipient")
	case s.AmountRao == 0 || s.AmountRao%UnitRao != 0:
		return fmt.Errorf("bridge: %d rao is not a positive whole number of %d rao units", s.AmountRao, UnitRao)
	case s.MaxFeeLamports == 0:
		return errors.New("bridge: send needs a fee cap")
	}
	return nil
}

// Data is the OFT program's send instruction data: its discriminator and
// SendParams (dst_eid, to, amount_ld, min_amount_ld, options, compose_msg,
// native_fee, lz_token_fee). The minimum equals the amount, since the bridge
// takes no fee and the amount is whole units.
func (s Send) Data() []byte {
	data := append([]byte(nil), sendDiscriminator...)
	data = binary.LittleEndian.AppendUint32(data, BittensorEID)
	data = append(data, make([]byte, 12)...)
	data = append(data, s.To[:]...)
	data = binary.LittleEndian.AppendUint64(data, s.AmountRao)
	data = binary.LittleEndian.AppendUint64(data, s.AmountRao)
	data = binary.LittleEndian.AppendUint32(data, uint32(len(typeThreeOptions)))
	data = append(data, typeThreeOptions...)
	data = append(data, 0) // no compose message
	data = binary.LittleEndian.AppendUint64(data, s.MaxFeeLamports)
	return binary.LittleEndian.AppendUint64(data, 0)
}

// Instruction is the OFT send with its accounts in order.
func (s Send) Instruction() (solana.Instruction, error) {
	if err := s.Validate(); err != nil {
		return solana.Instruction{}, err
	}
	source, err := solana.AssociatedTokenAddress(s.Sender, TaoMint)
	if err != nil {
		return solana.Instruction{}, err
	}
	accounts := make([]solana.AccountMeta, sendAccountCount)
	for i := range accounts {
		switch i {
		case 0, 22:
			accounts[i] = solana.AccountMeta{Key: s.Sender, Signer: true, Writable: true}
		case 3:
			accounts[i] = solana.AccountMeta{Key: source, Writable: true}
		default:
			pinned := sendAccounts[i]
			accounts[i] = solana.AccountMeta{Key: pinned.key, Writable: pinned.writable}
		}
	}
	return solana.Instruction{Program: OFTProgram, Accounts: accounts, Data: s.Data()}, nil
}

// Transaction compiles the send, with its compute budget, into an unsigned v0
// transaction paid by the sender.
func (s Send) Transaction(blockhash [32]byte, table *solana.LookupTable) (*solana.Transaction, error) {
	instruction, err := s.Instruction()
	if err != nil {
		return nil, err
	}
	limit := append([]byte{2}, binary.LittleEndian.AppendUint32(nil, computeUnitLimit)...)
	price := append([]byte{3}, binary.LittleEndian.AppendUint64(nil, computeUnitPriceMicroLamports)...)
	return solana.CompileV0(s.Sender, []solana.Instruction{
		{Program: solana.ComputeBudgetProgram, Data: limit},
		{Program: solana.ComputeBudgetProgram, Data: price},
		instruction,
	}, blockhash, []*solana.LookupTable{table})
}

// peerAddressOffset is where a PeerConfig account holds its peer, after the
// Anchor discriminator.
const peerAddressOffset = 8

// CheckPeerConfig refuses a peer config whose peer is not wTAO: a send would
// then deliver somewhere else.
func CheckPeerConfig(data []byte) error {
	if len(data) < peerAddressOffset+32 {
		return fmt.Errorf("bridge: peer config of %d bytes", len(data))
	}
	var want [32]byte
	copy(want[12:], WTAO[:])
	if [32]byte(data[peerAddressOffset:peerAddressOffset+32]) != want {
		return fmt.Errorf("bridge: the Solana peer is %x, not wTAO", data[peerAddressOffset:peerAddressOffset+32])
	}
	return nil
}

// Message statuses LayerZero Scan reports.
const (
	StatusDelivered = "DELIVERED"
	StatusFailed    = "FAILED"
	StatusBlocked   = "BLOCKED"
)

// Scan reads message status from the LayerZero Scan API.
type Scan struct {
	base string
	http *http.Client
}

// NewScan builds a Scan client for base; "" is LayerZero's public API.
func NewScan(base string) *Scan {
	if base == "" {
		base = defaultScanBase
	}
	return &Scan{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 30 * time.Second}}
}

// MessageStatus is the status and destination transaction of the message sent
// by a source transaction; empty until Scan has indexed it.
func (s *Scan) MessageStatus(ctx context.Context, sourceTx string) (status, destinationTx string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/messages/tx/"+sourceTx, nil)
	if err != nil {
		return "", "", fmt.Errorf("bridge: scan: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("bridge: scan: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("bridge: scan: read response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return "", "", fmt.Errorf("bridge: scan: response exceeds %d bytes", maxResponseBytes)
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", "", nil
	case resp.StatusCode != http.StatusOK:
		return "", "", fmt.Errorf("bridge: scan: HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Data []struct {
			Status struct {
				Name string `json:"name"`
			} `json:"status"`
			Destination struct {
				Tx struct {
					TxHash string `json:"txHash"`
				} `json:"tx"`
			} `json:"destination"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", "", fmt.Errorf("bridge: scan: decode: %w", err)
	}
	if len(envelope.Data) == 0 {
		return "", "", nil
	}
	return envelope.Data[0].Status.Name, envelope.Data[0].Destination.Tx.TxHash, nil
}
