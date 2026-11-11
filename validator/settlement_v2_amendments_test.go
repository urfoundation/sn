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

func amendmentsTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// amendmentsTestFile is a published file holding the signed extender entries
// for no 1 from each of epochs.
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
	previousURL, previousSigner := settlementV2AmendmentsURL, settlementV2AmendmentsSigner
	settlementV2AmendmentsURL = site.URL + "/amendments.yml"
	settlementV2AmendmentsSigner = func(chainID uint64, netuid uint16) (common.Address, bool) {
		return crypto.PubkeyToAddress(key.PublicKey), chainID == amendmentsTestChain && netuid == amendmentsTestNetuid
	}
	defer func() { settlementV2AmendmentsURL, settlementV2AmendmentsSigner = previousURL, previousSigner }()

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
	if raw, err := os.ReadFile(filepath.Join(stateDir, "amendments", "amendments.yml")); err != nil || len(raw) == 0 {
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
