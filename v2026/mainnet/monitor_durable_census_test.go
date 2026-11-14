// An intact root cannot silently reset retained finality/outage history.
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urfoundation/sn/v2026/internal/durablefixture"
)

// Metrics can be rendered again; this checkpoint owns non-reconstructible
// incident clocks and accepted finality and therefore cannot default to empty.
func TestMonitorDurableCensusRefusesLostCompletedCheckpoint(t *testing.T) {
	root := mainnetPrivateTestDir(t)
	fixture := durablefixture.New(t, t.Context(), root)
	path := filepath.Join(root, "monitor.json")
	provisionMonitorTestCustody(t, path)
	store, err := openMonitorCheckpoint(path, monitorTestExpectation(), fixture.Context)
	if err != nil {
		t.Fatal(err)
	}
	state := &monitorState{lastHash: testFinalizedHash, lastNumber: 100, lastProgressAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}
	if err := store.save(state); err != nil {
		store.close()
		t.Fatal(err)
	}
	if err := store.close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".retained"); err != nil {
		t.Fatal(err)
	}
	reopened, err := openMonitorCheckpoint(path, monitorTestExpectation(), fixture.Context)
	if err == nil {
		defer reopened.close()
		_, err = reopened.load()
	}
	if err == nil {
		t.Fatal("lost completed monitor history was reinitialized")
	}
}
