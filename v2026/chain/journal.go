package chain

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const (
	// JournalStageBroadcast records signed bytes handed to the node.
	JournalStageBroadcast = "broadcast"
	// JournalStageFinalized records a canonical, dispatch-successful inclusion.
	JournalStageFinalized = "finalized"
	// JournalStageFailed records a broadcast that did not reach finality, with
	// the error; the persisted raw bytes remain for inspection.
	JournalStageFailed = "failed"

	journalFileName = "native-transactions.jsonl"
	journalRawDir   = "native-transactions"
)

// JournalEntry is one line of the append-only native transaction journal.
type JournalEntry struct {
	Time           string `json:"time"`
	Command        string `json:"command"`
	Netuid         uint16 `json:"netuid"`
	Signer         string `json:"signer"`
	Hotkey         string `json:"hotkey"`
	Nonce          uint32 `json:"nonce"`
	ExtrinsicHash  string `json:"extrinsic_hash"`
	Stage          string `json:"stage"`
	BlockNumber    uint64 `json:"block_number,omitempty"`
	BlockHash      string `json:"block_hash,omitempty"`
	FeeEstimateRao uint64 `json:"fee_estimate_rao,omitempty"`
	FeeLimitRao    uint64 `json:"fee_limit_rao,omitempty"`
	Detail         string `json:"detail,omitempty"`
}

// Journal is a private (0700) directory holding native-transactions.jsonl and
// the exact SCALE bytes of every broadcast extrinsic.
type Journal struct {
	dir string
}

// OpenJournal creates the directory when absent and refuses a non-private one.
func OpenJournal(dir string) (*Journal, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("journal directory is empty")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("journal directory %s is not a private directory", abs)
	}
	if err := os.MkdirAll(filepath.Join(abs, journalRawDir), 0o700); err != nil {
		return nil, err
	}
	return &Journal{dir: abs}, nil
}

// Dir returns the journal directory.
func (self *Journal) Dir() string { return self.dir }

// Path returns the journal file path.
func (self *Journal) Path() string { return filepath.Join(self.dir, journalFileName) }

// Append writes one entry and syncs it before returning.
func (self *Journal) Append(entry JournalEntry) error {
	if self == nil {
		return errors.New("journal is nil")
	}
	if entry.Time == "" {
		entry.Time = time.Now().UTC().Format(time.RFC3339Nano)
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(self.Path(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return errors.Join(err, file.Close())
	}
	return errors.Join(file.Sync(), file.Close())
}

// SaveRaw persists the exact signed bytes under their extrinsic hash so an
// interrupted broadcast can be inspected or replayed byte-for-byte.
func (self *Journal) SaveRaw(hash types.Hash, raw []byte) error {
	if self == nil {
		return errors.New("journal is nil")
	}
	path := filepath.Join(self.dir, journalRawDir, strings.TrimPrefix(hash.Hex(), "0x")+".scale")
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// RawPath returns where SaveRaw stores an extrinsic's bytes.
func (self *Journal) RawPath(hash types.Hash) string {
	return filepath.Join(self.dir, journalRawDir, strings.TrimPrefix(hash.Hex(), "0x")+".scale")
}

// Entries reads every journal line; a missing journal is empty.
func (self *Journal) Entries() ([]JournalEntry, error) {
	if self == nil {
		return nil, errors.New("journal is nil")
	}
	file, err := os.Open(self.Path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var entries []JournalEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var entry JournalEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", self.Path(), line, err)
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}

// Hex32 renders a 32-byte key for journal and console output.
func Hex32(value [32]byte) string {
	return "0x" + hex.EncodeToString(value[:])
}
