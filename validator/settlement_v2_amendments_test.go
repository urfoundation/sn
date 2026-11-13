//go:build linux || darwin

// The ledger audit's reading of the signed settlement amendments (E_x) and
// its custody of extender components (WHITEPAPER.md §8.6.4, §8.6.7).
package validator

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/amendment"
	"github.com/urfoundation/sn/ledger"
)

const (
	amendmentsTestChain  = 964
	amendmentsTestNetuid = 25
)

// A fresh signing key for a test deployment's amendments.
func amendmentsTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// A published file holding the signed extender entries for no 1 from
// each of epochs.
func amendmentsTestFile(t *testing.T, key *ecdsa.PrivateKey, epochs ...uint64) []byte {
	t.Helper()
	var file strings.Builder
	file.WriteString("sn: 25\n")
	if len(epochs) > 0 {
		file.WriteString("signed_amendments:\n")
	}
	for _, epoch := range epochs {
		entry := amendment.NewExtenders(amendmentsTestChain, amendmentsTestNetuid, 1, epoch)
		if err := entry.Sign(key); err != nil {
			t.Fatal(err)
		}
		line, err := entry.Line()
		if err != nil {
			t.Fatal(err)
		}
		file.WriteString("  - line: \"" + line + "\"\n    signature: \"0x" + hex.EncodeToString(entry.Signature) + "\"\n")
	}
	return []byte(file.String())
}

// The test deployment's extender pool line for pool 1 from epoch.
func amendmentsTestLine(t *testing.T, epoch uint64) string {
	t.Helper()
	line, err := amendment.NewExtenders(amendmentsTestChain, amendmentsTestNetuid, 1, epoch).Line()
	if err != nil {
		t.Fatal(err)
	}
	return line
}

// E_x over a root's covered epochs is the first the amendment covers: the
// signed entry's, or a configured line's only where no signed entry is in
// force; 0 when it covers none of them. It is the same for every root that
// covers epochs from it, and 0 for a root wholly before it.
func TestSettlementV2ExtendersFrom(t *testing.T) {
	key := amendmentsTestKey(t)
	signer := crypto.PubkeyToAddress(key.PublicKey)
	parse := func(epochs ...uint64) *amendment.File {
		file, err := amendment.ParseVerified(amendmentsTestFile(t, key, epochs...), amendmentsTestChain, amendmentsTestNetuid, signer)
		if err != nil {
			t.Fatal(err)
		}
		return file
	}
	fallback := func(epochs ...uint64) *amendment.Fallback {
		lines := []string{}
		for _, epoch := range epochs {
			lines = append(lines, amendmentsTestLine(t, epoch))
		}
		parsed, err := amendment.ParseFallback(amendmentsTestChain, amendmentsTestNetuid, lines)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	for _, c := range []struct {
		name        string
		file        *amendment.File
		fallback    *amendment.Fallback
		first, last uint64
		want        uint64
	}{
		{name: "no entry", file: parse(), fallback: fallback(), first: 5, last: 12, want: 0},
		{name: "signed from 8, a root of epoch 8", file: parse(8), fallback: fallback(), first: 8, last: 8, want: 8},
		{name: "signed from 8, a root over 6 to 9", file: parse(8), fallback: fallback(), first: 6, last: 9, want: 8},
		{name: "signed from 8, a root over 9 and 10", file: parse(8), fallback: fallback(), first: 9, last: 10, want: 9},
		{name: "signed from 8, a root over 5 to 7", file: parse(8), fallback: fallback(), first: 5, last: 7, want: 0},
		{name: "configured from 8 only", file: parse(), fallback: fallback(8), first: 7, last: 8, want: 8},
		{name: "configured from 8, signed from 9", file: parse(9), fallback: fallback(8), first: 7, last: 9, want: 8},
		{name: "signed from 8, configured from 9", file: parse(8), fallback: fallback(9), first: 7, last: 9, want: 8},
		{name: "another operator's entry", file: parse(), fallback: nil, first: 7, last: 9, want: 0},
	} {
		got, err := settlementV2ExtendersFrom(c.file, c.fallback, 1, c.first, c.last)
		if err != nil || got != c.want {
			t.Fatalf("%s: E_x = %d, %v; want %d", c.name, got, err, c.want)
		}
	}
	// another pool's entry does not cover pool 2
	if got, err := settlementV2ExtendersFrom(parse(8), nil, 2, 7, 9); err != nil || got != 0 {
		t.Fatalf("pool 2: E_x = %d, %v", got, err)
	}
	// an unverified file is refused, as amendment.File.At refuses it
	unverified, err := amendment.ParseFile(amendmentsTestFile(t, key, 8))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settlementV2ExtendersFrom(unverified, nil, 1, 7, 9); !errors.Is(err, amendment.ErrUnverified) {
		t.Fatalf("an unverified file: %v", err)
	}
}

// A deployment with a pinned signer reads E_x from the published file it has
// verified, refreshed at most every interval and kept in state_dir; with none
// verified it is unknown, and the configured lines do not stand in. A file
// that cannot be fetched again leaves the verified one in force, and one
// signed by another key is never taken.
func TestSettlementV2AmendmentsPinnedDeployment(t *testing.T) {
	key := amendmentsTestKey(t)
	var served atomic.Value
	served.Store([]byte(nil))
	var fetches atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches.Add(1)
		raw := served.Load().([]byte)
		if raw == nil {
			http.NotFound(w, r)
			return
		}
		w.Write(raw)
	}))
	defer site.Close()
	previousUrl, previousSigner := settlementV2AmendmentsUrl, settlementV2AmendmentsSigner
	settlementV2AmendmentsUrl = site.URL + "/amendments.yml"
	settlementV2AmendmentsSigner = func(chainId uint64, netuid uint16) (common.Address, bool) {
		return crypto.PubkeyToAddress(key.PublicKey), chainId == amendmentsTestChain && netuid == amendmentsTestNetuid
	}
	defer func() { settlementV2AmendmentsUrl, settlementV2AmendmentsSigner = previousUrl, previousSigner }()

	ctx := context.Background()
	stateDir := t.TempDir()
	amendments, err := newSettlementV2Amendments(amendmentsTestChain, amendmentsTestNetuid, stateDir, []string{amendmentsTestLine(t, 8)})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	amendments.now = func() time.Time { return now }

	// Nothing published, nothing held: unknown, though a line is configured.
	if _, err := amendments.extendersFrom(ctx, 1, 7, 8); !errors.Is(err, amendment.ErrUnknown) {
		t.Fatalf("no verified file: %v, want ErrUnknown", err)
	}
	// The published file is taken and kept.
	served.Store(amendmentsTestFile(t, key, 8))
	if got, err := amendments.extendersFrom(ctx, 1, 7, 8); err != nil || got != 8 {
		t.Fatalf("E_x = %d, %v; want 8", got, err)
	}
	if raw, err := os.ReadFile(settlementV2AmendmentsStorePath(stateDir)); err != nil || len(raw) == 0 {
		t.Fatalf("the verified file is not kept: %v", err)
	}
	// Within the interval nothing is fetched.
	before := fetches.Load()
	served.Store(amendmentsTestFile(t, amendmentsTestKey(t), 7))
	if got, err := amendments.extendersFrom(ctx, 1, 9, 9); err != nil || got != 9 || fetches.Load() != before {
		t.Fatalf("within the interval: E_x = %d, %v, %d fetches", got, err, fetches.Load()-before)
	}
	// A file signed by another key, here from epoch 7, is never taken: the
	// held file, from epoch 8, stays in force.
	now = now.Add(settlementV2AmendmentsRefresh)
	if got, err := amendments.extendersFrom(ctx, 1, 7, 7); err != nil || got != 0 || fetches.Load() != before+1 {
		t.Fatalf("another signer's file: E_x = %d, %v, %d fetches", got, err, fetches.Load()-before)
	}
	// So does an outage.
	now = now.Add(settlementV2AmendmentsRefresh)
	served.Store([]byte(nil))
	if got, err := amendments.extendersFrom(ctx, 1, 7, 9); err != nil || got != 8 || fetches.Load() != before+2 {
		t.Fatalf("after an outage: E_x = %d, %v", got, err)
	}
	// A restarted validator reads the kept file while the site is down.
	restarted, err := newSettlementV2Amendments(amendmentsTestChain, amendmentsTestNetuid, stateDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := restarted.extendersFrom(ctx, 1, 8, 8); err != nil || got != 8 {
		t.Fatalf("restarted: E_x = %d, %v", got, err)
	}
}

// A deployment without a pinned signer publishes no amendments: only its
// configured lines put one in force. A malformed configured line stops the
// audit from being built.
func TestSettlementV2AmendmentsUnpinnedDeployment(t *testing.T) {
	ctx := context.Background()
	none, err := newSettlementV2Amendments(945, 350, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := none.extendersFrom(ctx, 1, 1, 100); err != nil || got != 0 {
		t.Fatalf("E_x = %d, %v; want none", got, err)
	}
	line, err := amendment.NewExtenders(945, 350, 1, 8).Line()
	if err != nil {
		t.Fatal(err)
	}
	configured, err := newSettlementV2Amendments(945, 350, t.TempDir(), []string{line})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := configured.extendersFrom(ctx, 1, 6, 9); err != nil || got != 8 {
		t.Fatalf("E_x = %d, %v; want 8", got, err)
	}
	if _, err := newSettlementV2Amendments(945, 350, t.TempDir(), []string{strings.Replace(line, "zeta 1", "zeta 2", 1)}); err == nil {
		t.Fatal("a malformed configured line was accepted")
	}
	if _, err := newSettlementV2Amendments(945, 350, t.TempDir(), []string{amendmentsTestLine(t, 8)}); err == nil {
		t.Fatal("a line for another deployment was accepted")
	}
}

// --amendments-fallback reads one line per line of its file, past blank
// lines and comments.
func TestReadSettlementV2AmendmentsFallback(t *testing.T) {
	if lines, err := readSettlementV2AmendmentsFallback(""); err != nil || lines != nil {
		t.Fatalf("no flag: %v %v", lines, err)
	}
	path := filepath.Join(t.TempDir(), "fallback.txt")
	line := amendmentsTestLine(t, 8)
	if err := os.WriteFile(path, []byte("# the configured extender pool\n\n  "+line+"  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if lines, err := readSettlementV2AmendmentsFallback(path); err != nil || !slices.Equal(lines, []string{line}) {
		t.Fatalf("lines %v, %v", lines, err)
	}
	if _, err := readSettlementV2AmendmentsFallback(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("an absent file was read")
	}
}

// Custody keeps an epoch's valid extender component, refuses one recorded
// for another pool epoch or malformed, and reads it back exactly.
func TestSettlementV2CustodyRecordsExtenders(t *testing.T) {
	custody := newSettlementV2CustodyTest(t)
	ctx := context.Background()
	if _, ok, err := custody.ValidExtenders(ctx, 1, 8); ok || err != nil {
		t.Fatalf("a component held before it was kept: %v", err)
	}
	for _, want := range []*ledger.HeaderExtenders{
		{Bytes: 1_000_000_000_001, Census: []ledger.Ref{ledger.RefOf([]byte("c"))}, Rows: []ledger.Ref{ledger.RefOf([]byte("r"))}},
		{Bytes: 0, Census: []ledger.Ref{}, Rows: []ledger.Ref{}},
		{Bytes: ^uint64(0), Census: []ledger.Ref{ledger.RefOf([]byte("c1")), ledger.RefOf([]byte("c2"))}, Rows: []ledger.Ref{}},
	} {
		if err := custody.KeepValidExtenders(ctx, 1, 8, want); err != nil {
			t.Fatal(err)
		}
		if held, ok, err := custody.ValidExtenders(ctx, 1, 8); err != nil || !ok || !held.Equal(want) {
			t.Fatalf("held %+v, %v, %v; want %+v", held, ok, err, want)
		}
	}
	if _, ok, err := custody.ValidExtenders(ctx, 1, 9); ok || err != nil {
		t.Fatalf("another epoch's component: %v", err)
	}
	for name, record := range map[string]string{
		"another pool":    `{"schema":"` + settlementV2CustodySchema + `","no_id":2,"epoch":8,"bytes":"1","census":[],"rows":[]}`,
		"no census":       `{"schema":"` + settlementV2CustodySchema + `","no_id":1,"epoch":8,"bytes":"1","census":null,"rows":[]}`,
		"bad bytes":       `{"schema":"` + settlementV2CustodySchema + `","no_id":1,"epoch":8,"bytes":"-1","census":[],"rows":[]}`,
		"a bad reference": `{"schema":"` + settlementV2CustodySchema + `","no_id":1,"epoch":8,"bytes":"1","census":["sha256:00"],"rows":[]}`,
	} {
		if err := atomicStateWrite(custody.path("extenders", "1-8.json"), []byte(record), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, ok, err := custody.ValidExtenders(ctx, 1, 8); err == nil || ok {
			t.Fatalf("%s: the record was accepted", name)
		}
	}
}

// A published file of the test deployment the steering tests pin (chain 945,
// sn 521), holding the extender entries for no 1 from each of epochs, signed
// by the test price key both readers pin for it.
func cacheTestFile(t *testing.T, epochs ...uint64) []byte {
	t.Helper()
	var file strings.Builder
	file.WriteString("sn: 521\nsigned_amendments:\n")
	for _, epoch := range epochs {
		entry := amendment.NewExtenders(depositPriceTestChainID, 521, 1, epoch)
		if err := entry.Sign(depositPriceTestKey); err != nil {
			t.Fatal(err)
		}
		line, err := entry.Line()
		if err != nil {
			t.Fatal(err)
		}
		file.WriteString("  - line: \"" + line + "\"\n    signature: \"0x" + hex.EncodeToString(entry.Signature) + "\"\n")
	}
	return []byte(file.String())
}

// Serves whatever file it is given, and nothing while it holds none.
func cacheTestSite(t *testing.T) (*httptest.Server, *atomic.Value) {
	t.Helper()
	var served atomic.Value
	served.Store([]byte(nil))
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := served.Load().([]byte)
		if raw == nil {
			http.Error(w, "synthetic outage", http.StatusServiceUnavailable)
			return
		}
		w.Write(raw)
	}))
	t.Cleanup(site.Close)
	return site, &served
}

// The ledger audit and capture the flag's steering read the same published file,
// and each keeps its verified copy in its own place in one state directory:
// state_dir/settlement-v2-amendments/amendments.yml and
// state_dir/amendments/amendments.yml. Neither ever reads or writes the other's,
// so the files each holds, kept or restarted from, are only its own, and one
// reader's copy never stands in for the other's while a site is down.
func TestSettlementV2AmendmentsCacheIsApartFromCaptureTheFlags(t *testing.T) {
	ctx := context.Background()
	const chainID, netuid = depositPriceTestChainID, uint16(521)
	signer := crypto.PubkeyToAddress(depositPriceTestKey.PublicKey)
	extenderSite, extenderServed := cacheTestSite(t)
	ctfSite, ctfServed := cacheTestSite(t)
	previousUrl, previousSigner := settlementV2AmendmentsUrl, settlementV2AmendmentsSigner
	settlementV2AmendmentsUrl = extenderSite.URL + "/amendments.yml"
	settlementV2AmendmentsSigner = func(chain uint64, sn uint16) (common.Address, bool) {
		return signer, chain == chainID && sn == netuid
	}
	defer func() { settlementV2AmendmentsUrl, settlementV2AmendmentsSigner = previousUrl, previousSigner }()

	stateDir := t.TempDir()
	extenderPath := settlementV2AmendmentsStorePath(stateDir)
	// Capture the flag's own place, as release_capture_the_flag.go keeps it.
	ctfPath := filepath.Join(stateDir, "amendments", "amendments.yml")
	if filepath.Dir(extenderPath) == filepath.Dir(ctfPath) {
		t.Fatalf("the ledger audit keeps its copy in capture the flag's directory %s", filepath.Dir(ctfPath))
	}
	// newExtender and newCtf build each reader as a validator does: the ledger
	// audit's from its constructor, capture the flag's from its constructor with
	// its source pointed at the test site and its own store.
	newExtender := func() *settlementV2Amendments {
		t.Helper()
		reader, err := newSettlementV2Amendments(chainID, netuid, stateDir, nil)
		if err != nil {
			t.Fatal(err)
		}
		return reader
	}
	newCtf := func() *releaseAmendments {
		t.Helper()
		reader, err := newReleaseAmendments(&ReleaseConfig{ChainID: chainID, Netuid: netuid, StateDir: stateDir})
		if err != nil || reader.source == nil {
			t.Fatalf("capture the flag's reader: %v", err)
		}
		reader.source = amendment.NewSource(ctfSite.URL+"/amendments.yml", chainID, netuid, signer, &releaseAmendmentStore{path: ctfPath})
		return reader
	}
	held := func(name string, file *amendment.File) int {
		t.Helper()
		if file == nil {
			t.Fatalf("%s holds no verified file", name)
		}
		return len(file.Entries())
	}
	stored := func(path string) []byte {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return raw
	}
	short, long := cacheTestFile(t, 8), cacheTestFile(t, 8, 9)

	// Each takes what its own site publishes and keeps it in its own place: the
	// ledger audit the longer file, capture the flag the shorter.
	extenderServed.Store(long)
	ctfServed.Store(short)
	extender, ctf := newExtender(), newCtf()
	if from, err := extender.extendersFrom(ctx, 1, 8, 8); err != nil || from != 8 {
		t.Fatalf("E_x = %d, %v", from, err)
	}
	ctf.refresh(ctx)
	if held("the ledger audit", extender.file) != 2 || held("capture the flag", ctf.file) != 1 {
		t.Fatal("a reader holds another's file")
	}
	if !slices.Equal(stored(extenderPath), long) || !slices.Equal(stored(ctfPath), short) {
		t.Fatal("a reader wrote the other's copy")
	}

	// Each refreshes from its own site only. Capture the flag takes the longer
	// file, which keeps its entry; the ledger audit refuses the shorter one, which
	// drops one of its own, and both keep exactly what they hold.
	extenderServed.Store(short)
	ctfServed.Store(long)
	later := time.Now().Add(2 * max(settlementV2AmendmentsRefresh, releaseAmendmentCacheTTL))
	extender.now, ctf.now = func() time.Time { return later }, func() time.Time { return later }
	if from, err := extender.extendersFrom(ctx, 1, 9, 9); err != nil || from != 9 {
		t.Fatalf("E_x = %d, %v", from, err)
	}
	ctf.refresh(ctx)
	if held("the ledger audit", extender.file) != 2 || held("capture the flag", ctf.file) != 2 {
		t.Fatal("a refresh took another's file or walked one back")
	}
	if !slices.Equal(stored(extenderPath), long) || !slices.Equal(stored(ctfPath), long) {
		t.Fatal("a refresh changed the other reader's copy")
	}

	// A restart with both sites down recovers each reader's own copy.
	extenderServed.Store([]byte(nil))
	ctfServed.Store([]byte(nil))
	if err := atomicStateWrite(ctfPath, short, 0o600); err != nil {
		t.Fatal(err)
	}
	extender, ctf = newExtender(), newCtf()
	if from, err := extender.extendersFrom(ctx, 1, 9, 9); err != nil || from != 9 || held("the restarted ledger audit", extender.file) != 2 {
		t.Fatalf("the restarted ledger audit: E_x = %d, %v", from, err)
	}
	ctf.refresh(ctx)
	if held("restarted capture the flag", ctf.file) != 1 {
		t.Fatal("capture the flag restarted from another's copy")
	}

	// A copy in the other reader's place never stands in for a reader's own: with
	// its site down and nothing of its own kept, each holds no verified file, and
	// writes nothing.
	for _, owner := range []string{"capture the flag", "the ledger audit"} {
		stateDir = t.TempDir()
		extenderPath, ctfPath = settlementV2AmendmentsStorePath(stateDir), filepath.Join(stateDir, "amendments", "amendments.yml")
		kept, other := ctfPath, extenderPath
		if owner == "the ledger audit" {
			kept, other = extenderPath, ctfPath
		}
		if err := atomicStateWrite(kept, long, 0o600); err != nil {
			t.Fatal(err)
		}
		extender, ctf := newExtender(), newCtf()
		_, err := extender.extendersFrom(ctx, 1, 8, 8)
		ctf.refresh(ctx)
		if owner == "capture the flag" {
			if !errors.Is(err, amendment.ErrUnknown) || extender.file != nil {
				t.Fatalf("the ledger audit read capture the flag's copy: %v", err)
			}
			if held("capture the flag", ctf.file) != 2 {
				t.Fatal("capture the flag lost its own copy")
			}
		} else {
			if ctf.file != nil {
				t.Fatal("capture the flag read the ledger audit's copy")
			}
			if err != nil || held("the ledger audit", extender.file) != 2 {
				t.Fatalf("the ledger audit lost its own copy: %v", err)
			}
		}
		if _, err := os.Stat(other); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("with %s's copy kept, the other reader wrote one it never verified: %v", owner, err)
		}
		if !slices.Equal(stored(kept), long) {
			t.Fatalf("%s's copy changed", owner)
		}
	}
}

// The ledger audit reads the repository's own amendments.yml, the file the site
// publishes byte for byte, under SN25's real pinned signer: it verifies, keeps it,
// and, with no signed entry in it yet, puts the extender pool in force only through
// a configured line. A copy with an entry the pinned key never signed is not taken.
func TestSettlementV2AmendmentsReadsTheRepositoryFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "amendments.yml"))
	if err != nil {
		t.Fatal(err)
	}
	site, served := cacheTestSite(t)
	served.Store(raw)
	previousUrl, previousSigner := settlementV2AmendmentsUrl, settlementV2AmendmentsSigner
	settlementV2AmendmentsUrl, settlementV2AmendmentsSigner = site.URL+"/amendments.yml", amendment.SignerFor
	defer func() { settlementV2AmendmentsUrl, settlementV2AmendmentsSigner = previousUrl, previousSigner }()
	ctx := context.Background()

	stateDir := t.TempDir()
	reader, err := newSettlementV2Amendments(amendmentsTestChain, amendmentsTestNetuid, stateDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if reader.source == nil {
		t.Fatal("SN25 reads no published file")
	}
	file := reader.held(ctx)
	if file == nil {
		t.Fatal("the repository's file was not taken")
	}
	// Whatever the file holds by now verifies; while it holds no entry, no epoch is
	// covered.
	from, err := reader.extendersFrom(ctx, 1, 6, 9)
	if err != nil || (len(file.Entries()) == 0 && from != 0) {
		t.Fatalf("E_x = %d, %v", from, err)
	}
	if kept, err := os.ReadFile(settlementV2AmendmentsStorePath(stateDir)); err != nil || !slices.Equal(kept, raw) {
		t.Fatalf("the kept copy differs from the published bytes: %v", err)
	}
	if len(file.Entries()) == 0 {
		configured, err := newSettlementV2Amendments(amendmentsTestChain, amendmentsTestNetuid, t.TempDir(), []string{amendmentsTestLine(t, 8)})
		if err != nil {
			t.Fatal(err)
		}
		if from, err := configured.extendersFrom(ctx, 1, 6, 9); err != nil || from != 8 {
			t.Fatalf("the configured line: E_x = %d, %v", from, err)
		}
	}

	// An entry signed by another key is never taken, and with nothing verified the
	// amendments are unknown.
	served.Store(amendmentsTestFile(t, amendmentsTestKey(t), 8))
	forged, err := newSettlementV2Amendments(amendmentsTestChain, amendmentsTestNetuid, t.TempDir(), []string{amendmentsTestLine(t, 8)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := forged.extendersFrom(ctx, 1, 6, 9); !errors.Is(err, amendment.ErrUnknown) {
		t.Fatalf("a file the pinned key never signed: %v", err)
	}
}
