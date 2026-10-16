package ledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
)

const (
	// Schema names the header format (VERSION2-FINAL.md §6.5).
	Schema = "urnetwork-payout-ledger-v2"
	// ProviderRowBytes is the size of a provider row (§6.2), big-endian:
	// client_id(16) ‖ coldkey(32) ‖ usage_bytes(8) ‖ head_excluded(1).
	ProviderRowBytes = 57
	// AccountRowBytes is the size of an account row: coldkey(32) ‖ cumulative(16).
	AccountRowBytes = 48
	// ChunkRows is the row count of every chunk of a list except the last,
	// which holds 1 to ChunkRows rows. An empty list has no chunks.
	ChunkRows = 131_072
	// MaxHeaderBytes bounds a header.
	MaxHeaderBytes = 256 << 10
	// MaxCoveredEpochs bounds the epochs one root covers.
	MaxCoveredEpochs = 96
)

// maxHeaderBytes is MaxHeaderBytes. Tests lower it to reach the builder's
// oversized-header refusal, which otherwise needs about 3,500 chunks.
var maxHeaderBytes = MaxHeaderBytes

var (
	// ErrRef is returned for an object reference that is not "sha256:"
	// followed by 64 lowercase hex digits.
	ErrRef = errors.New("ledger: object reference must be sha256: and 64 lowercase hex digits")
	// ErrHeaderTooLarge is returned for a header over 256 KiB.
	ErrHeaderTooLarge = errors.New("ledger: header exceeds 256 KiB")
	// ErrHeaderDecode is returned for a header that is not JSON of the header
	// shape: a syntax error, an unknown field, or a value of the wrong type,
	// such as an integer written as 1.0, 1e3, -1 or "1".
	ErrHeaderDecode = errors.New("ledger: header does not decode")
	// ErrHeaderTrailingData is returned for any byte after the header's JSON
	// value, a newline included.
	ErrHeaderTrailingData = errors.New("ledger: header has trailing data")
	// ErrHeaderNotCanonical is returned when re-encoding the decoded header
	// does not reproduce its bytes: whitespace, key order or case, a missing
	// or repeated key, an escaped string, or null in place of a number or a
	// string.
	ErrHeaderNotCanonical = errors.New("ledger: header is not canonical")
	// ErrHeaderSchema is returned for a schema other than Schema.
	ErrHeaderSchema = errors.New("ledger: header schema is not " + Schema)
	// ErrHeaderNull is returned for "epochs" or "accounts" written as null.
	ErrHeaderNull = errors.New("ledger: header has null outside providers")
	// ErrTooManyEpochs is returned for more than 96 covered epochs.
	ErrTooManyEpochs = errors.New("ledger: more than 96 covered epochs")
	// ErrChunkLength is returned for a chunk that is not a whole number of
	// rows, or holds more than ChunkRows rows.
	ErrChunkLength = errors.New("ledger: chunk is not 1 to 131,072 whole rows")
	// ErrChunkShort is returned for a chunk before the last that is not full.
	ErrChunkShort = errors.New("ledger: a chunk before the last is not full")
	// ErrChunkEmpty is returned for an empty last chunk.
	ErrChunkEmpty = errors.New("ledger: the last chunk is empty")
	// ErrRowEncoding is returned for a provider row whose head_excluded byte
	// is not 0 or 1.
	ErrRowEncoding = errors.New("ledger: provider row head_excluded is not 0 or 1")
	// ErrRowZeroClientID is returned for a provider row with a zero client id.
	ErrRowZeroClientID = errors.New("ledger: provider row has a zero client id")
	// ErrRowZeroUsage is returned for a provider row with zero usage.
	ErrRowZeroUsage = errors.New("ledger: provider row has zero usage")
	// ErrRowsUnsorted is returned for provider rows out of client id order.
	ErrRowsUnsorted = errors.New("ledger: provider rows are not sorted by client id")
	// ErrRowDuplicate is returned for two provider rows with one client id.
	ErrRowDuplicate = errors.New("ledger: provider rows repeat a client id")
	// ErrUsageOverflow is returned for an epoch whose usage over all rows
	// reaches 2^64, which v1 refuses too (§6.3).
	ErrUsageOverflow = errors.New("ledger: provider rows' total usage reaches 2^64")
)

var valueLimit = new(uint256.Int).Lsh(uint256.NewInt(1), 128)

// Ref names an object by the sha256 of its bytes. Its text form is "sha256:"
// followed by 64 lowercase hex digits.
type Ref [32]byte

// RefOf returns the reference of data.
func RefOf(data []byte) Ref {
	return sha256.Sum256(data)
}

// String returns the text form of r.
func (r Ref) String() string {
	return "sha256:" + hex.EncodeToString(r[:])
}

// ParseRef parses the text form of a reference.
func ParseRef(s string) (Ref, error) {
	var r Ref
	if len(s) != 71 || s[:7] != "sha256:" {
		return r, ErrRef
	}
	for i := 7; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return r, ErrRef
		}
	}
	hex.Decode(r[:], []byte(s[7:]))
	return r, nil
}

// EncodeProviderChunks encodes rows, in the order given, as a chunk list.
func EncodeProviderChunks(rows []protocol.LedgerUsageRow) [][]byte {
	return encodeChunks(len(rows), ProviderRowBytes, func(b []byte, i int) []byte {
		row := &rows[i]
		b = append(b, row.ClientID[:]...)
		b = append(b, row.Coldkey[:]...)
		b = binary.BigEndian.AppendUint64(b, row.UsageBytes)
		if row.HeadExcluded {
			return append(b, 1)
		}
		return append(b, 0)
	})
}

// DecodeProviderChunks decodes a chunk list, checking its shape and each
// row's encoding (§9.2 check 4). It does not check the §6.2 row rules.
func DecodeProviderChunks(chunks [][]byte) ([]protocol.LedgerUsageRow, error) {
	var rows []protocol.LedgerUsageRow
	for j, chunk := range chunks {
		if err := checkChunk(len(chunk), ProviderRowBytes, j, len(chunks)); err != nil {
			return nil, err
		}
		var err error
		if rows, err = decodeProviderRows(rows, chunk); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

// EncodeAccountChunks encodes accounts, in position order, as a chunk list.
// Every cumulative must be below 2^128.
func EncodeAccountChunks(accounts []merkle.LedgerAccount) ([][]byte, error) {
	for i := range accounts {
		if !accounts[i].Cumulative.Lt(valueLimit) {
			return nil, fmt.Errorf("%w: account %d", merkle.ErrLedgerValueRange, i)
		}
	}
	return encodeAccountChunks(accounts), nil
}

// DecodeAccountChunks decodes a chunk list of account rows, checking its shape.
func DecodeAccountChunks(chunks [][]byte) ([]merkle.LedgerAccount, error) {
	var accounts []merkle.LedgerAccount
	for j, chunk := range chunks {
		if err := checkChunk(len(chunk), AccountRowBytes, j, len(chunks)); err != nil {
			return nil, err
		}
		accounts = decodeAccountRows(accounts, chunk)
	}
	return accounts, nil
}

func encodeAccountChunks(accounts []merkle.LedgerAccount) [][]byte {
	return encodeChunks(len(accounts), AccountRowBytes, func(b []byte, i int) []byte {
		b = append(b, accounts[i].Coldkey[:]...)
		value := accounts[i].Cumulative.Bytes32()
		return append(b, value[16:]...)
	})
}

func encodeChunks(n, rowBytes int, appendRow func([]byte, int) []byte) [][]byte {
	chunks := make([][]byte, 0, (n+ChunkRows-1)/ChunkRows)
	for start := 0; start < n; start += ChunkRows {
		end := min(start+ChunkRows, n)
		chunk := make([]byte, 0, (end-start)*rowBytes)
		for i := start; i < end; i++ {
			chunk = appendRow(chunk, i)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

// checkChunk checks the length of chunk j of a list of m chunks.
func checkChunk(length, rowBytes, j, m int) error {
	if length%rowBytes != 0 || length > ChunkRows*rowBytes {
		return fmt.Errorf("%w: chunk %d has %d bytes", ErrChunkLength, j, length)
	}
	if j < m-1 && length != ChunkRows*rowBytes {
		return fmt.Errorf("%w: chunk %d of %d has %d rows", ErrChunkShort, j, m, length/rowBytes)
	}
	if j == m-1 && length == 0 {
		return fmt.Errorf("%w: chunk %d", ErrChunkEmpty, j)
	}
	return nil
}

func decodeProviderRows(rows []protocol.LedgerUsageRow, chunk []byte) ([]protocol.LedgerUsageRow, error) {
	for offset := 0; offset < len(chunk); offset += ProviderRowBytes {
		b := chunk[offset : offset+ProviderRowBytes]
		var row protocol.LedgerUsageRow
		copy(row.ClientID[:], b[:16])
		copy(row.Coldkey[:], b[16:48])
		row.UsageBytes = binary.BigEndian.Uint64(b[48:56])
		switch b[56] {
		case 0:
		case 1:
			row.HeadExcluded = true
		default:
			return nil, fmt.Errorf("%w: byte %d", ErrRowEncoding, offset+56)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func decodeAccountRows(accounts []merkle.LedgerAccount, chunk []byte) []merkle.LedgerAccount {
	for offset := 0; offset < len(chunk); offset += AccountRowBytes {
		var account merkle.LedgerAccount
		copy(account.Coldkey[:], chunk[offset:offset+32])
		account.Cumulative.SetBytes(chunk[offset+32 : offset+AccountRowBytes])
		accounts = append(accounts, account)
	}
	return accounts
}

// checkProviderRows checks the §6.2 rules a verifier can check (§9.2 check 5):
// every row has a nonzero client id and positive usage, the rows are sorted
// by client id and unique, and their usage sums below 2^64 (§6.3).
func checkProviderRows(rows []protocol.LedgerUsageRow) error {
	var total uint64
	for i := range rows {
		row := &rows[i]
		if row.ClientID == ([16]byte{}) {
			return fmt.Errorf("%w: row %d", ErrRowZeroClientID, i)
		}
		if row.UsageBytes == 0 {
			return fmt.Errorf("%w: row %d", ErrRowZeroUsage, i)
		}
		if i > 0 {
			switch bytes.Compare(rows[i-1].ClientID[:], row.ClientID[:]) {
			case 0:
				return fmt.Errorf("%w: row %d", ErrRowDuplicate, i)
			case 1:
				return fmt.Errorf("%w: row %d", ErrRowsUnsorted, i)
			}
		}
		if total+row.UsageBytes < total {
			return fmt.Errorf("%w: row %d", ErrUsageOverflow, i)
		}
		total += row.UsageBytes
	}
	return nil
}

// Header is a root's header (§6.5). ledgerHash, committed on chain, is the
// sha256 of its encoding.
type Header struct {
	NoID   uint64
	Epoch  uint64
	Epochs []HeaderEpoch
	// Accounts lists the account chunks.
	Accounts []Ref
}

// HeaderEpoch is one covered epoch of a header.
type HeaderEpoch struct {
	Epoch uint64
	// Null declares the epoch's inputs unrecoverable ("providers": null).
	Null bool
	// Providers lists the epoch's provider chunks; empty when Null, or when
	// the epoch has no rows ("providers": []).
	Providers []Ref
}

// headerJSON is a header as JSON decodes it, before any reference is parsed.
// A nil slice stands for null.
type headerJSON struct {
	Schema   string            `json:"schema"`
	NoID     uint64            `json:"no_id"`
	Epoch    uint64            `json:"epoch"`
	Epochs   []headerEpochJSON `json:"epochs"`
	Accounts []string          `json:"accounts"`
}

type headerEpochJSON struct {
	Epoch     uint64   `json:"epoch"`
	Providers []string `json:"providers"`
}

// appendJSON writes the canonical form: compact, keys in order, integers as
// JSON numbers. Strings are written without escaping; a header whose
// strings need escaping fails the schema or reference rules, so the bytes
// accepted are exactly the canonical JSON.
func (h *headerJSON) appendJSON(b []byte) []byte {
	b = append(b, `{"schema":"`...)
	b = append(b, h.Schema...)
	b = append(b, `","no_id":`...)
	b = strconv.AppendUint(b, h.NoID, 10)
	b = append(b, `,"epoch":`...)
	b = strconv.AppendUint(b, h.Epoch, 10)
	b = append(b, `,"epochs":`...)
	if h.Epochs == nil {
		b = append(b, "null"...)
	} else {
		b = append(b, '[')
		for i := range h.Epochs {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, `{"epoch":`...)
			b = strconv.AppendUint(b, h.Epochs[i].Epoch, 10)
			b = append(b, `,"providers":`...)
			b = appendStrings(b, h.Epochs[i].Providers)
			b = append(b, '}')
		}
		b = append(b, ']')
	}
	b = append(b, `,"accounts":`...)
	b = appendStrings(b, h.Accounts)
	return append(b, '}')
}

func appendStrings(b []byte, values []string) []byte {
	if values == nil {
		return append(b, "null"...)
	}
	b = append(b, '[')
	for i, s := range values {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '"')
		b = append(b, s...)
		b = append(b, '"')
	}
	return append(b, ']')
}

// EncodeHeader returns the canonical bytes of h.
func EncodeHeader(h *Header) ([]byte, error) {
	if len(h.Epochs) > MaxCoveredEpochs {
		return nil, ErrTooManyEpochs
	}
	raw := headerJSON{Schema: Schema, NoID: h.NoID, Epoch: h.Epoch, Epochs: make([]headerEpochJSON, len(h.Epochs)), Accounts: refStrings(h.Accounts)}
	for i := range h.Epochs {
		raw.Epochs[i].Epoch = h.Epochs[i].Epoch
		if !h.Epochs[i].Null {
			raw.Epochs[i].Providers = refStrings(h.Epochs[i].Providers)
		}
	}
	data := raw.appendJSON(nil)
	if len(data) > maxHeaderBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrHeaderTooLarge, len(data))
	}
	return data, nil
}

func refStrings(refs []Ref) []string {
	values := make([]string, len(refs))
	for i := range refs {
		values[i] = refs[i].String()
	}
	return values
}

// DecodeHeader decodes a canonical header. It decodes with unknown fields
// refused and requires that re-encoding reproduce the bytes exactly
// (§6.5), then checks the schema, that null appears only for providers, the
// epoch count and every reference.
func DecodeHeader(data []byte) (*Header, error) {
	if len(data) > maxHeaderBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrHeaderTooLarge, len(data))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var raw headerJSON
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrHeaderDecode, err)
	}
	if decoder.InputOffset() != int64(len(data)) {
		return nil, ErrHeaderTrailingData
	}
	if !bytes.Equal(raw.appendJSON(nil), data) {
		return nil, ErrHeaderNotCanonical
	}
	if raw.Schema != Schema {
		return nil, ErrHeaderSchema
	}
	if raw.Epochs == nil || raw.Accounts == nil {
		return nil, ErrHeaderNull
	}
	if len(raw.Epochs) > MaxCoveredEpochs {
		return nil, ErrTooManyEpochs
	}
	h := &Header{NoID: raw.NoID, Epoch: raw.Epoch, Epochs: make([]HeaderEpoch, len(raw.Epochs))}
	var err error
	for i := range raw.Epochs {
		epoch := &h.Epochs[i]
		epoch.Epoch, epoch.Null = raw.Epochs[i].Epoch, raw.Epochs[i].Providers == nil
		if epoch.Providers, err = parseRefs(raw.Epochs[i].Providers); err != nil {
			return nil, err
		}
	}
	if h.Accounts, err = parseRefs(raw.Accounts); err != nil {
		return nil, err
	}
	return h, nil
}

func parseRefs(values []string) ([]Ref, error) {
	refs := make([]Ref, len(values))
	for i, s := range values {
		var err error
		if refs[i], err = ParseRef(s); err != nil {
			return nil, fmt.Errorf("%w: reference %d", err, i)
		}
	}
	return refs, nil
}
