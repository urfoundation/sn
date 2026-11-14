package amendment

import (
	"crypto/ecdsa"
	"math/big"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	testChain  uint64 = 964
	testNetuid uint16 = 25
	// testCarry stands in for the carry contract: EIP-55's own example address, whose
	// checksum mixes cases.
	testCarry = "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"
	// The lines of the design: WHITEPAPER.md §8.6.7's and CAPTURETHEFLAG.md §2.4's,
	// with testCarry in place of the carry contract.
	extenderLine = "UR settlement amendment v1: chain 964, sn 25, no 1, from epoch 8, intra-platform and intra-network extenders, units egress prefix, free and paid, zeta 1"
	ctfLine      = "UR settlement amendment v2: chain 964, sn 25, no 1, from epoch 8, capture the flag, ctf pool 4294967297, carry " + testCarry + ", shares pool 4/10 ctf 1/10 head 5/10, head cedes to pool, slots 5 providers 5 extenders, countries iso 3166-1 249, presence half epoch, units egress prefix, draw drand quicknet 60s after census"
)

func testKey(t testing.TB) (*ecdsa.PrivateKey, common.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key, crypto.PubkeyToAddress(key.PublicKey)
}

func mustParse(t testing.TB, line string) *Entry {
	t.Helper()
	entry, err := ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func signed(t testing.TB, key *ecdsa.PrivateKey, entry *Entry) *Entry {
	t.Helper()
	if err := entry.Sign(key); err != nil {
		t.Fatal(err)
	}
	return entry
}

// The lines the design documents give are the lines this package reads, with their
// terms as typed values.
func TestTheDesignLinesParse(t *testing.T) {
	extenders := mustParse(t, extenderLine)
	want := &Entry{Version: V1, ChainID: 964, Netuid: 25, NoID: 1, FromEpoch: 8, Extender: &ExtenderTerms{
		IntraPlatform: true, IntraNetwork: true, Units: UnitsEgressPrefix, FreeAndPaid: true, Zeta: Ratio{Num: 1, Den: 1},
	}}
	if *extenders.Extender != *want.Extender || extenders.Ctf != nil || extenders.Version != want.Version || extenders.ChainID != 964 || extenders.Netuid != 25 || extenders.NoID != 1 || extenders.FromEpoch != 8 {
		t.Fatalf("%+v %+v", extenders, extenders.Extender)
	}
	if extenders.Kind() != KindExtenders || extenders.Configured() || extenders.Signature != nil {
		t.Fatal(extenders.Kind(), extenders.Configured())
	}
	ctf := mustParse(t, ctfLine)
	wantTerms := CtfTerms{
		CtfPool:         4_294_967_297,
		Carry:           common.HexToAddress(testCarry),
		Shares:          Shares{Pool: Ratio{Num: 4, Den: 10}, Ctf: Ratio{Num: 1, Den: 10}, Head: Ratio{Num: 5, Den: 10}},
		HeadCedesToPool: true,
		ProviderSlots:   5,
		ExtenderSlots:   5,
		Countries:       249,
		Presence:        Ratio{Num: 1, Den: 2},
		Units:           UnitsEgressPrefix,
		DrawDelay:       time.Minute,
	}
	if ctf.Ctf == nil || *ctf.Ctf != wantTerms || ctf.Extender != nil || ctf.Version != V2 || ctf.ChainID != 964 || ctf.Netuid != 25 || ctf.NoID != 1 || ctf.FromEpoch != 8 {
		t.Fatalf("%+v %+v", ctf, ctf.Ctf)
	}
	if ctf.Kind() != KindCaptureTheFlag {
		t.Fatal(ctf.Kind())
	}
	// The constructors write the same lines, and each reads back as written.
	for line, entry := range map[string]*Entry{
		extenderLine: NewExtenders(964, 25, 1, 8),
		ctfLine:      NewCaptureTheFlag(964, 25, 1, 8, common.HexToAddress(testCarry)),
	} {
		written, err := entry.Line()
		if err != nil || written != line {
			t.Fatalf("got  %q, %v\nwant %q", written, err, line)
		}
		for _, c := range []byte(line) {
			if c < 0x20 || c > 0x7e {
				t.Fatalf("a hardware wallet could not show byte %x", c)
			}
		}
	}
	// And they are the lines the design documents give, the carry contract aside.
	for document, line := range map[string]string{
		"../WHITEPAPER.md":     extenderLine,
		"../CAPTURETHEFLAG.md": strings.Replace(ctfLine, testCarry, "0x<carry contract>", 1),
	} {
		raw, err := os.ReadFile(document)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), line) {
			t.Fatalf("%s does not give the line %q", document, line)
		}
	}
}

// Other operators, epochs and deployments are lines too, each operator with its own
// CTF pool.
func TestTheVariableTerms(t *testing.T) {
	for _, entry := range []*Entry{
		NewExtenders(945, 1, 4_294_967_295, 0),
		NewExtenders(1<<64-1, 65_535, 2, 1<<64-1),
		NewCaptureTheFlag(964, 25, 2, 9, common.HexToAddress(testCarry)),
		NewCaptureTheFlag(964, 25, 4_294_967_295, 1<<64-1, common.HexToAddress("0x0000000000000000000000000000000000000001")),
	} {
		line, err := entry.Line()
		if err != nil {
			t.Fatal(err)
		}
		parsed := mustParse(t, line)
		if again, err := parsed.Line(); err != nil || again != line {
			t.Fatal(again, err)
		}
		if parsed.NoID != entry.NoID || parsed.FromEpoch != entry.FromEpoch || parsed.ChainID != entry.ChainID || parsed.Netuid != entry.Netuid {
			t.Fatalf("%+v", parsed)
		}
		if parsed.Ctf != nil && parsed.Ctf.CtfPool != 1<<32+entry.NoID {
			t.Fatal(parsed.Ctf.CtfPool)
		}
	}
}

// An operator's CTF pool is 2^32 + its id, and only an operator id has one.
func TestCtfPoolIDs(t *testing.T) {
	for noID, pool := range map[uint64]uint64{1: 4_294_967_297, 2: 4_294_967_298, 1<<32 - 1: 1<<33 - 1} {
		if got, ok := CtfPoolID(noID); !ok || got != pool {
			t.Fatalf("no %d: %d %t", noID, got, ok)
		}
		if got, ok := CtfNoID(pool); !ok || got != noID {
			t.Fatalf("pool %d: %d %t", pool, got, ok)
		}
	}
	for _, noID := range []uint64{0, 1 << 32, 1<<64 - 1, 1<<64 - 1<<32 + 1} {
		if pool, ok := CtfPoolID(noID); ok {
			t.Fatalf("no %d has pool %d", noID, pool)
		}
	}
	for _, pool := range []uint64{0, 1, 1<<32 - 1, 1 << 32, 1 << 33, 1<<64 - 1} {
		if noID, ok := CtfNoID(pool); ok {
			t.Fatalf("pool %d is no %d's", pool, noID)
		}
	}
}

// replaceClause swaps the clause that starts with prefix.
func replaceClause(t testing.TB, line, prefix, next string) string {
	t.Helper()
	head, rest, _ := strings.Cut(line, ": ")
	clauses := strings.Split(rest, ", ")
	for i, clause := range clauses {
		if strings.HasPrefix(clause, prefix) {
			clauses[i] = next
			return head + ": " + strings.Join(clauses, ", ")
		}
	}
	t.Fatalf("no clause %q in %q", prefix, line)
	return ""
}

// Lines that are almost right are refused, whatever part is wrong.
func TestNearMissLinesAreRefused(t *testing.T) {
	v1, v2 := extenderLine, ctfLine
	misses := map[string]string{
		"empty":                    "",
		"too long":                 v2 + strings.Repeat(" ", maxLineBytes),
		"trailing space":           v2 + " ",
		"leading space":            " " + v2,
		"trailing comma":           v2 + ",",
		"trailing period":          v1 + ".",
		"trailing newline":         v1 + "\n",
		"carriage return":          v1 + "\r",
		"a tab":                    strings.Replace(v1, "zeta 1", "zeta\t1", 1),
		"a NUL":                    v1 + "\x00",
		"a non-ASCII hyphen":       strings.Replace(v2, "3166-1", "3166‑1", 1),
		"a non-ASCII digit":        strings.Replace(v1, "zeta 1", "zeta １", 1),
		"lowercase UR":             strings.Replace(v1, "UR ", "ur ", 1),
		"capital Settlement":       strings.Replace(v1, "settlement", "Settlement", 1),
		"no version":               strings.Replace(v1, " v1:", ":", 1),
		"capital V":                strings.Replace(v1, " v1:", " V1:", 1),
		"space in the version":     strings.Replace(v1, " v1:", " v 1:", 1),
		"space before the colon":   strings.Replace(v1, "v1:", "v1 :", 1),
		"no space after the colon": strings.Replace(v1, "v1: ", "v1:", 1),
		"version 01":               strings.Replace(v1, "v1:", "v01:", 1),
		"version 0":                strings.Replace(v1, "v1:", "v0:", 1),
		"version 3":                strings.Replace(v2, "v2:", "v3:", 1),
		"version 256":              strings.Replace(v2, "v2:", "v256:", 1),
		"a huge version":           strings.Replace(v2, "v2:", "v18446744073709551617:", 1),
		"v1's terms under v2":      strings.Replace(v1, "v1:", "v2:", 1),
		"v2's terms under v1":      strings.Replace(v2, "v2:", "v1:", 1),
		"a semicolon":              strings.Replace(v2, ", capture", "; capture", 1),
		"a comma without a space":  strings.Replace(v2, ", capture", ",capture", 1),
		"two spaces":               strings.Replace(v2, ", capture", ",  capture", 1),
		"a space before a comma":   strings.Replace(v2, ", capture", " , capture", 1),
		"chain 0":                  replaceClause(t, v1, "chain ", "chain 0"),
		"chain 0964":               replaceClause(t, v1, "chain ", "chain 0964"),
		"chain past uint64":        replaceClause(t, v1, "chain ", "chain 18446744073709551616"),
		"chain in hex":             replaceClause(t, v1, "chain ", "chain 0x3c4"),
		"chain with a sign":        replaceClause(t, v1, "chain ", "chain +964"),
		"chain with a separator":   replaceClause(t, v1, "chain ", "chain 9_64"),
		"no chain value":           replaceClause(t, v1, "chain ", "chain "),
		"sn 0":                     replaceClause(t, v1, "sn ", "sn 0"),
		"sn 025":                   replaceClause(t, v1, "sn ", "sn 025"),
		"sn past uint16":           replaceClause(t, v1, "sn ", "sn 65536"),
		"netuid for sn":            replaceClause(t, v1, "sn ", "netuid 25"),
		"no 0":                     replaceClause(t, v1, "no ", "no 0"),
		"no 2^32":                  replaceClause(t, v1, "no ", "no 4294967296"),
		"no 01":                    replaceClause(t, v2, "no ", "no 01"),
		"no -1":                    replaceClause(t, v2, "no ", "no -1"),
		"epoch 08":                 replaceClause(t, v1, "from epoch ", "from epoch 08"),
		"epoch 8.0":                replaceClause(t, v1, "from epoch ", "from epoch 8.0"),
		"epoch -8":                 replaceClause(t, v1, "from epoch ", "from epoch -8"),
		"epoch 1e1":                replaceClause(t, v1, "from epoch ", "from epoch 1e1"),
		"epoch past uint64":        replaceClause(t, v1, "from epoch ", "from epoch 18446744073709551616"),
		"epoch with words":         replaceClause(t, v1, "from epoch ", "from epoch eight"),
		"from without epoch":       replaceClause(t, v1, "from epoch ", "from 8"),
		"zeta 1.0":                 replaceClause(t, v1, "zeta ", "zeta 1.0"),
		"zeta 2":                   replaceClause(t, v1, "zeta ", "zeta 2"),
		"zeta 01":                  replaceClause(t, v1, "zeta ", "zeta 01"),
		"paid only":                replaceClause(t, v1, "free and paid", "paid"),
		"intra-platform only":      replaceClause(t, v1, "intra-platform", "intra-platform extenders"),
		"asn units":                replaceClause(t, v1, "units ", "units asn"),
		"another operator's pool":  replaceClause(t, v2, "ctf pool ", "ctf pool 4294967298"),
		"the operator for a pool":  replaceClause(t, v2, "ctf pool ", "ctf pool 1"),
		"pool 01":                  replaceClause(t, v2, "ctf pool ", "ctf pool 04294967297"),
		"no pool value":            replaceClause(t, v2, "ctf pool ", "ctf pool "),
		"pool without ctf":         replaceClause(t, v2, "ctf pool ", "pool 4294967297"),
		"carry in lowercase":       replaceClause(t, v2, "carry ", "carry "+strings.ToLower(testCarry)),
		"carry in uppercase":       replaceClause(t, v2, "carry ", "carry 0x"+strings.ToUpper(testCarry[2:])),
		"carry, one digit off":     replaceClause(t, v2, "carry ", "carry "+testCarry[:41]+"e"),
		"carry, a case swapped":    replaceClause(t, v2, "carry ", "carry "+testCarry[:3]+"A"+testCarry[4:]),
		"carry without 0x":         replaceClause(t, v2, "carry ", "carry "+testCarry[2:]),
		"carry with 0X":            replaceClause(t, v2, "carry ", "carry 0X"+testCarry[2:]),
		"carry short":              replaceClause(t, v2, "carry ", "carry "+testCarry[:41]),
		"carry long":               replaceClause(t, v2, "carry ", "carry "+testCarry+"d"),
		"carry zero":               replaceClause(t, v2, "carry ", "carry 0x0000000000000000000000000000000000000000"),
		"carry placeholder":        replaceClause(t, v2, "carry ", "carry 0x<carry contract>"),
		"carry missing":            replaceClause(t, v2, "carry ", "carry"),
		"shares as decimals":       replaceClause(t, v2, "shares ", "shares pool 0.4 ctf 0.1 head 0.5"),
		"shares reduced":           replaceClause(t, v2, "shares ", "shares pool 2/5 ctf 1/10 head 1/2"),
		"shares scaled":            replaceClause(t, v2, "shares ", "shares pool 8/20 ctf 2/20 head 10/20"),
		"shares reordered":         replaceClause(t, v2, "shares ", "shares ctf 1/10 pool 4/10 head 5/10"),
		"other shares":             replaceClause(t, v2, "shares ", "shares pool 3/10 ctf 2/10 head 5/10"),
		"shares not summing":       replaceClause(t, v2, "shares ", "shares pool 4/10 ctf 1/10 head 4/10"),
		"shares with spaces":       replaceClause(t, v2, "shares ", "shares pool 4 / 10 ctf 1 / 10 head 5 / 10"),
		"head cedes to ctf":        replaceClause(t, v2, "head cedes", "head cedes to ctf"),
		"head cedes to pools":      replaceClause(t, v2, "head cedes", "head cedes to pools"),
		"six provider slots":       replaceClause(t, v2, "slots ", "slots 6 providers 5 extenders"),
		"slots reordered":          replaceClause(t, v2, "slots ", "slots 5 extenders 5 providers"),
		"250 countries":            replaceClause(t, v2, "countries ", "countries iso 3166-1 250"),
		"iso without its part":     replaceClause(t, v2, "countries ", "countries iso 3166 249"),
		"alpha-2 named":            replaceClause(t, v2, "countries ", "countries iso 3166-1 alpha-2 249"),
		"presence a third":         replaceClause(t, v2, "presence ", "presence third epoch"),
		"presence 1/2":             replaceClause(t, v2, "presence ", "presence 1/2 epoch"),
		"draw after 61s":           replaceClause(t, v2, "draw ", "draw drand quicknet 61s after census"),
		"draw after 60 s":          replaceClause(t, v2, "draw ", "draw drand quicknet 60 s after census"),
		"draw after 1m":            replaceClause(t, v2, "draw ", "draw drand quicknet 1m after census"),
		"draw on mainnet drand":    replaceClause(t, v2, "draw ", "draw drand mainnet 60s after census"),
		"draw before census":       replaceClause(t, v2, "draw ", "draw drand quicknet 60s before census"),
		"a CTF line for v1 terms":  strings.Replace(v1, "intra-platform and intra-network extenders", "capture the flag", 1),
		"v1 with a CTF clause":     strings.Replace(v1, ", zeta 1", ", zeta 1, capture the flag", 1),
		"v2 without carry":         strings.Replace(v2, ", carry "+testCarry, "", 1),
	}
	// Every clause dropped, doubled or swapped with the next.
	for name, line := range map[string]string{"v1": v1, "v2": v2} {
		head, rest, _ := strings.Cut(line, ": ")
		clauses := strings.Split(rest, ", ")
		join := func(parts []string) string { return head + ": " + strings.Join(parts, ", ") }
		for i := range clauses {
			misses[name+" without clause "+clauses[i]] = join(slices.Delete(slices.Clone(clauses), i, i+1))
			misses[name+" with clause "+clauses[i]+" twice"] = join(slices.Insert(slices.Clone(clauses), i, clauses[i]))
			if i+1 < len(clauses) && clauses[i] != clauses[i+1] {
				swapped := slices.Clone(clauses)
				swapped[i], swapped[i+1] = swapped[i+1], swapped[i]
				misses[name+" with "+clauses[i]+" after the next"] = join(swapped)
			}
		}
	}
	for name, line := range misses {
		if line == extenderLine || line == ctfLine {
			t.Fatalf("%s: the case is no miss", name)
		}
		if entry, err := ParseLine(line); err == nil {
			t.Errorf("%s: parsed %q as %+v", name, line, entry)
		}
	}
}

// valueWords come before a line's values, each of which runs to its clause's end.
var valueWords = []string{"chain ", "sn ", "no ", "from epoch ", "ctf pool ", "carry "}

// fixedPositions are the byte offsets of a line that its version fixes: everything
// but the digits of its values.
func fixedPositions(line string) []int {
	head, rest, _ := strings.Cut(line, ": ")
	var fixed []int
	for i := range len(head) + 2 {
		fixed = append(fixed, i)
	}
	offset := len(head) + 2
	clauses := strings.Split(rest, ", ")
	for n, clause := range clauses {
		words := len(clause)
		for _, prefix := range valueWords {
			if strings.HasPrefix(clause, prefix) {
				words = len(prefix)
			}
		}
		for i := range words {
			fixed = append(fixed, offset+i)
		}
		offset += len(clause)
		if n+1 < len(clauses) {
			fixed = append(fixed, offset, offset+1) // the separator
			offset += 2
		}
	}
	return fixed
}

// No single change to a line's fixed words, of any character, reads as a line: not a
// letter dropped, changed or added, nor a space or comma.
func TestEveryOneCharacterChangeToTheWordsIsRefused(t *testing.T) {
	for _, line := range []string{extenderLine, ctfLine} {
		positions := fixedPositions(line)
		if len(positions) < len(line)/2 {
			t.Fatalf("only %d fixed positions in %q", len(positions), line)
		}
		for _, at := range positions {
			other := byte('x')
			if line[at] == other {
				other = 'y'
			}
			upper := line[at]
			if upper >= 'a' && upper <= 'z' {
				upper -= 'a' - 'A'
			} else if upper >= 'A' && upper <= 'Z' {
				upper += 'a' - 'A'
			} else {
				upper = '_'
			}
			for name, changed := range map[string]string{
				"dropped":     line[:at] + line[at+1:],
				"changed":     line[:at] + string(other) + line[at+1:],
				"recased":     line[:at] + string(upper) + line[at+1:],
				"space added": line[:at] + " " + line[at:],
				"comma added": line[:at] + "," + line[at:],
			} {
				if entry, err := ParseLine(changed); err == nil {
					t.Errorf("byte %d %s: parsed %q as %+v", at, name, changed, entry)
				}
			}
		}
	}
}

// A signature binds every term of its line, for one deployment and one signer.
func TestTheSignatureBindsTheLine(t *testing.T) {
	key, signer := testKey(t)
	for _, line := range []string{extenderLine, ctfLine} {
		entry := signed(t, key, mustParse(t, line))
		if err := entry.Verify(testChain, testNetuid, signer); err != nil {
			t.Fatal(err)
		}
		if len(entry.Signature) != 65 || entry.Signature[64] != 27 && entry.Signature[64] != 28 {
			t.Fatalf("not as a hardware wallet returns it: %x", entry.Signature)
		}
		changes := map[string]func(*Entry){
			"first epoch": func(e *Entry) { e.FromEpoch++ },
			"chain":       func(e *Entry) { e.ChainID = 945 },
			"subnet":      func(e *Entry) { e.Netuid = 26 },
		}
		if entry.Version == V2 {
			changes["operator"] = func(e *Entry) { e.NoID, e.Ctf.CtfPool = 2, ctfPoolBase+2 }
			changes["carry"] = func(e *Entry) { e.Ctf.Carry = common.HexToAddress("0x0000000000000000000000000000000000000001") }
			changes["version"] = func(e *Entry) { e.Version, e.Extender, e.Ctf = V1, NewExtenders(1, 1, 1, 1).Extender, nil }
		} else {
			changes["operator"] = func(e *Entry) { e.NoID = 2 }
		}
		for name, change := range changes {
			changed := entry.clone()
			change(changed)
			// Verified as the deployment the changed entry names, so only the
			// signature can refuse it.
			if err := changed.Verify(changed.ChainID, changed.Netuid, signer); err == nil {
				t.Fatalf("%s: a changed entry verified", name)
			}
		}
		if err := entry.Verify(945, testNetuid, signer); err == nil {
			t.Fatal("verified for another chain")
		}
		if err := entry.Verify(testChain, 26, signer); err == nil {
			t.Fatal("verified for another subnet")
		}
		_, other := testKey(t)
		if err := entry.Verify(testChain, testNetuid, other); err == nil || !strings.Contains(err.Error(), signer.Hex()) {
			t.Fatal("verified under another signer:", err)
		}
		if err := entry.Verify(testChain, testNetuid, common.Address{}); err == nil {
			t.Fatal("verified under no signer")
		}
	}
}

// Terms no line states cannot be signed or verified, and so cannot slip past a
// signature over the line that does state them.
func TestTermsALineDoesNotStateAreRefused(t *testing.T) {
	key, signer := testKey(t)
	for name, change := range map[string]func(*Entry){
		"shares":        func(e *Entry) { e.Ctf.Shares.Ctf = Ratio{Num: 2, Den: 10} },
		"no head cede":  func(e *Entry) { e.Ctf.HeadCedesToPool = false },
		"slots":         func(e *Entry) { e.Ctf.ProviderSlots = 6 },
		"countries":     func(e *Entry) { e.Ctf.Countries = 250 },
		"presence":      func(e *Entry) { e.Ctf.Presence = Ratio{Num: 2, Den: 4} },
		"units":         func(e *Entry) { e.Ctf.Units = "asn" },
		"draw delay":    func(e *Entry) { e.Ctf.DrawDelay = time.Minute + time.Second },
		"pool":          func(e *Entry) { e.Ctf.CtfPool++ },
		"no carry":      func(e *Entry) { e.Ctf.Carry = common.Address{} },
		"no terms":      func(e *Entry) { e.Ctf = nil },
		"both terms":    func(e *Entry) { e.Extender = NewExtenders(1, 1, 1, 1).Extender },
		"version 3":     func(e *Entry) { e.Version = 3 },
		"no chain":      func(e *Entry) { e.ChainID = 0 },
		"no subnet":     func(e *Entry) { e.Netuid = 0 },
		"operator 0":    func(e *Entry) { e.NoID = 0 },
		"operator 2^32": func(e *Entry) { e.NoID, e.Ctf.CtfPool = 1<<32, ctfPoolBase+1<<32 },
		"extender zeta": func(e *Entry) {
			e.Version, e.Extender, e.Ctf = V1, &ExtenderTerms{IntraPlatform: true, IntraNetwork: true, Units: UnitsEgressPrefix, FreeAndPaid: true, Zeta: Ratio{Num: 2, Den: 2}}, nil
		},
		"extender units": func(e *Entry) {
			e.Version, e.Extender, e.Ctf = V1, &ExtenderTerms{IntraPlatform: true, Units: UnitsEgressPrefix, FreeAndPaid: true, Zeta: Ratio{Num: 1, Den: 1}}, nil
		},
	} {
		entry := mustParse(t, ctfLine)
		change(entry)
		if line, err := entry.Line(); err == nil {
			t.Fatalf("%s: wrote %q", name, line)
		}
		if err := entry.Sign(key); err == nil {
			t.Fatalf("%s: signed", name)
		}
		good := signed(t, key, mustParse(t, ctfLine))
		entry.Signature = good.Signature
		if err := entry.Verify(testChain, testNetuid, signer); err == nil {
			t.Fatalf("%s: verified under the line's signature", name)
		}
	}
}

// malleate is the other signature of the same message by the same key: s mirrored
// and the recovery byte flipped.
func malleate(signature []byte) []byte {
	out := slices.Clone(signature)
	s := new(big.Int).Sub(crypto.S256().Params().N, new(big.Int).SetBytes(out[32:64]))
	copy(out[32:64], common.LeftPadBytes(s.Bytes(), 32))
	out[64] = 27 + 28 - out[64]
	return out
}

// A signature counts only in the one form a hardware wallet returns: 65 bytes, its
// recovery byte 27 or 28, s in the lower half. The other encoding of the same
// signature, and signatures over anything but the line as text, are refused.
func TestOnlyACanonicalSignatureOfTheTextCounts(t *testing.T) {
	key, signer := testKey(t)
	entry := signed(t, key, mustParse(t, ctfLine))
	if err := entry.Verify(testChain, testNetuid, signer); err != nil {
		t.Fatal(err)
	}
	refuse := func(name string, signature []byte) {
		t.Helper()
		changed := entry.clone()
		changed.Signature = signature
		if err := changed.Verify(testChain, testNetuid, signer); err == nil {
			t.Fatalf("%s: verified", name)
		}
	}
	malleated := malleate(entry.Signature)
	if public, err := crypto.SigToPub(textHash(ctfLine), append(slices.Clone(malleated[:64]), malleated[64]-27)); err != nil || crypto.PubkeyToAddress(*public) != signer {
		t.Fatal("the malleated signature does not recover the signer, so it tests nothing", err)
	}
	refuse("the malleated signature", malleated)
	raw := slices.Clone(entry.Signature)
	raw[64] -= 27
	refuse("a recovery byte of 0 or 1", raw)
	high := slices.Clone(entry.Signature)
	high[64] += 2
	refuse("a recovery byte of 29 or 30", high)
	refuse("no signature", nil)
	refuse("64 bytes", entry.Signature[:64])
	refuse("66 bytes", append(slices.Clone(entry.Signature), 0))
	zero := make([]byte, 65)
	zero[64] = 27
	refuse("zeros", zero)
	bytesSigned, err := crypto.Sign(crypto.Keccak256([]byte(ctfLine)), key)
	if err != nil {
		t.Fatal(err)
	}
	bytesSigned[64] += 27
	refuse("the line's bytes, not as text", bytesSigned)
	otherLine := signed(t, key, mustParse(t, extenderLine))
	refuse("another line's signature", otherLine.Signature)
}
