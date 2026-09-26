package chain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

func TestJournalAppendsAndReadsBackStages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "native")
	journal, err := OpenJournal(dir)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("journal directory mode = %v, %v", info.Mode(), err)
	}
	hash := ExtrinsicHash([]byte{1, 2, 3})
	if err := journal.SaveRaw(hash, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(journal.RawPath(hash))
	if err != nil || string(raw) != "\x01\x02\x03" {
		t.Fatalf("raw bytes = %x, %v", raw, err)
	}
	if info, err := os.Stat(journal.RawPath(hash)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("raw file mode = %v, %v", info, err)
	}
	for _, stage := range []string{JournalStageBroadcast, JournalStageFinalized} {
		if err := journal.Append(JournalEntry{Command: "validator register", Netuid: 25, Signer: "5x", Hotkey: Hex32([32]byte{9}), Nonce: 4, ExtrinsicHash: hash.Hex(), Stage: stage, BlockNumber: 12, BlockHash: types.Hash{5}.Hex()}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := journal.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Stage != JournalStageBroadcast || entries[1].Stage != JournalStageFinalized || entries[1].ExtrinsicHash != hash.Hex() || entries[1].Time == "" {
		t.Fatalf("entries = %+v", entries)
	}
	if info, err := os.Stat(journal.Path()); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("journal mode = %v, %v", info, err)
	}
	empty, err := OpenJournal(filepath.Join(t.TempDir(), "empty"))
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := empty.Entries(); err != nil || len(entries) != 0 {
		t.Fatalf("empty journal = %v, %v", entries, err)
	}
}
