package amendment

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/demand/pricesheet"
)

// launchFile is a file before any entry is signed.
const launchFile = `sn: 25
# Settlement amendments for SN25, each signed by the key that signs price.yml.
`

func extendersLine(t testing.TB, noID, epoch uint64) string {
	t.Helper()
	line, err := NewExtenders(testChain, testNetuid, noID, epoch).Line()
	if err != nil {
		t.Fatal(err)
	}
	return line
}

func ctfLineAt(t testing.TB, noID, epoch uint64) string {
	t.Helper()
	line, err := NewCaptureTheFlag(testChain, testNetuid, noID, epoch, common.HexToAddress(testCarry)).Line()
	if err != nil {
		t.Fatal(err)
	}
	return line
}

// fileText is a file of lines, each signed by key, as attach writes them.
func fileText(t testing.TB, key *ecdsa.PrivateKey, lines ...string) string {
	t.Helper()
	var file strings.Builder
	file.WriteString(launchFile)
	if len(lines) > 0 {
		file.WriteString("signed_amendments:\n")
	}
	for _, line := range lines {
		entry := signed(t, key, mustParse(t, line))
		fmt.Fprintf(&file, "  - line: \"%s\"\n    signature: \"0x%s\"\n", line, hex.EncodeToString(entry.Signature))
	}
	return file.String()
}

func verifiedFile(t testing.TB, raw string, signer common.Address) *File {
	t.Helper()
	file, err := ParseFile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Verify(testChain, testNetuid, signer); err != nil {
		t.Fatal(err)
	}
	return file
}

// fromEpoch is an answer's first epoch, or -1 for none in force.
func fromEpoch(entry *Entry) int64 {
	if entry == nil {
		return -1
	}
	return int64(entry.FromEpoch)
}

// The repository's own amendments.yml, whatever it holds by now, verifies under the
// real pinned signer. It is the file the site publishes.
func TestTheRepositoryFileVerifies(t *testing.T) {
	raw, err := os.ReadFile("../amendments.yml")
	if err != nil {
		t.Fatal(err)
	}
	file, err := ParseFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	signer, pinned := SignerFor(testChain, testNetuid)
	if !pinned {
		t.Fatal("SN25 has no pinned signer")
	}
	if err := file.Verify(testChain, testNetuid, signer); err != nil {
		t.Fatal(err)
	}
}

// The amendments are signed by the key that signs the price sheet, and only SN25's
// deployment has one.
func TestTheSignerIsThePriceSheets(t *testing.T) {
	signer, ok := SignerFor(964, 25)
	if !ok || signer != pricesheet.MainnetSigner || signer != common.HexToAddress("0x16C372dbBb24cd8473345ab13971E40814C8658F") {
		t.Fatal(signer, ok)
	}
	if _, ok := SignerFor(945, 25); ok {
		t.Fatal("testnet has a pinned signer")
	}
	if _, ok := SignerFor(964, 26); ok {
		t.Fatal("another subnet has a pinned signer")
	}
}

// Each kind's entry applies from its first epoch, for its own operator, until a later
// entry of that kind takes over. Kinds and operators are independent.
func TestEntriesInForceByEpoch(t *testing.T) {
	key, signer := testKey(t)
	file := verifiedFile(t, fileText(t, key,
		extendersLine(t, 1, 8),
		ctfLineAt(t, 1, 8),
		extendersLine(t, 2, 10),
		ctfLineAt(t, 1, 12),
	), signer)
	for _, c := range []struct {
		noID, epoch    uint64
		extenders, ctf int64
	}{
		{noID: 1, epoch: 0, extenders: -1, ctf: -1},
		{noID: 1, epoch: 7, extenders: -1, ctf: -1},
		{noID: 1, epoch: 8, extenders: 8, ctf: 8},
		{noID: 1, epoch: 9, extenders: 8, ctf: 8},
		{noID: 1, epoch: 11, extenders: 8, ctf: 8},
		{noID: 1, epoch: 12, extenders: 8, ctf: 12},
		{noID: 1, epoch: 1 << 40, extenders: 8, ctf: 12},
		{noID: 2, epoch: 9, extenders: -1, ctf: -1},
		{noID: 2, epoch: 10, extenders: 10, ctf: -1},
		{noID: 2, epoch: 12, extenders: 10, ctf: -1},
		{noID: 3, epoch: 100, extenders: -1, ctf: -1},
	} {
		in, err := file.At(c.noID, c.epoch, nil)
		if err != nil {
			t.Fatal(err)
		}
		if fromEpoch(in.Extenders) != c.extenders || fromEpoch(in.CaptureTheFlag) != c.ctf {
			t.Errorf("no %d, epoch %d: extenders from %d, ctf from %d; want %d and %d", c.noID, c.epoch, fromEpoch(in.Extenders), fromEpoch(in.CaptureTheFlag), c.extenders, c.ctf)
		}
		for _, entry := range []*Entry{in.Extenders, in.CaptureTheFlag} {
			if entry != nil && (entry.NoID != c.noID || entry.Configured() || entry.Verify(testChain, testNetuid, signer) != nil) {
				t.Errorf("no %d, epoch %d: %+v", c.noID, c.epoch, entry)
			}
		}
		if in.Extenders != nil && in.Extenders.Kind() != KindExtenders || in.CaptureTheFlag != nil && in.CaptureTheFlag.Kind() != KindCaptureTheFlag {
			t.Errorf("no %d, epoch %d: an answer of the wrong kind", c.noID, c.epoch)
		}
	}
	// The answer is a copy: changing it changes nothing a later reader sees.
	in, _ := file.At(1, 12, nil)
	in.CaptureTheFlag.FromEpoch = 1
	in.CaptureTheFlag.Ctf.Carry = common.Address{}
	in.CaptureTheFlag.Signature[0] ^= 1
	again, _ := file.At(1, 12, nil)
	if again.CaptureTheFlag.FromEpoch != 12 || again.CaptureTheFlag.Ctf.Carry != common.HexToAddress(testCarry) || again.CaptureTheFlag.Verify(testChain, testNetuid, signer) != nil {
		t.Fatalf("%+v", again.CaptureTheFlag)
	}
	// So are the entries it lists.
	entries := file.Entries()
	if len(entries) != 4 || entries[3].FromEpoch != 12 || file.Netuid() != testNetuid {
		t.Fatalf("%+v", entries)
	}
	entries[3].FromEpoch = 13
	entries[3].Signature[1] ^= 1
	if again, _ := file.At(1, 12, nil); again.CaptureTheFlag == nil || again.CaptureTheFlag.FromEpoch != 12 || again.CaptureTheFlag.Verify(testChain, testNetuid, signer) != nil {
		t.Fatalf("%+v", again.CaptureTheFlag)
	}
}

// Nothing answers from a file nobody verified, or from no file.
func TestOnlyAVerifiedFileAnswers(t *testing.T) {
	key, signer := testKey(t)
	raw := fileText(t, key, extendersLine(t, 1, 8))
	file, err := ParseFile([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.At(1, 8, nil); !errors.Is(err, ErrUnverified) {
		t.Fatal("an unverified file answered:", err)
	}
	_, other := testKey(t)
	if err := file.Verify(testChain, testNetuid, other); err == nil {
		t.Fatal("verified under another signer")
	}
	if _, err := file.At(1, 8, nil); !errors.Is(err, ErrUnverified) {
		t.Fatal("a file that failed verification answered:", err)
	}
	if err := file.Verify(testChain, testNetuid, signer); err != nil {
		t.Fatal(err)
	}
	if in, err := file.At(1, 8, nil); err != nil || in.Extenders == nil {
		t.Fatal(in, err)
	}
	// A later failed verification takes the answer back.
	if err := file.Verify(945, testNetuid, signer); err == nil {
		t.Fatal("verified for another chain")
	}
	if _, err := file.At(1, 8, nil); !errors.Is(err, ErrUnverified) {
		t.Fatal("a file that failed its last verification answered:", err)
	}
	// A file made any other way than by parsing has no entries to answer with.
	if _, err := (&File{}).At(1, 8, nil); !errors.Is(err, ErrUnverified) {
		t.Fatal("a file nobody parsed or verified answered:", err)
	}
	fallback, err := ParseFallback(testChain, testNetuid, []string{extendersLine(t, 1, 8)})
	if err != nil {
		t.Fatal(err)
	}
	var none *File
	if _, err := none.At(1, 8, fallback); !errors.Is(err, ErrUnknown) {
		t.Fatal("no file answered, from configuration or otherwise:", err)
	}
	if err := none.Verify(testChain, testNetuid, signer); err == nil {
		t.Fatal("verified no file")
	}
}

// Configuration stands in only where no signed entry of its kind is in force: before
// the first signed entry, for a signing that came too late for its epoch.
func TestTheFallbackStandsInOnlyWhereNoSignedEntryIs(t *testing.T) {
	key, signer := testKey(t)
	// The signing missed epoch 8, so configuration covers it and the entry signed
	// next starts at 9. The extender pool's configuration says 6, its entry 8.
	file := verifiedFile(t, fileText(t, key, extendersLine(t, 1, 8), ctfLineAt(t, 1, 9)), signer)
	fallback, err := ParseFallback(testChain, testNetuid, []string{
		ctfLineAt(t, 1, 8),
		extendersLine(t, 1, 6),
		extendersLine(t, 1, 20), // never used: a signed entry is in force from 8
		extendersLine(t, 2, 5),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		noID, epoch         uint64
		extenders, ctf      int64
		extendersConfigured bool
		ctfConfigured       bool
	}{
		{noID: 1, epoch: 5, extenders: -1, ctf: -1},
		{noID: 1, epoch: 6, extenders: 6, ctf: -1, extendersConfigured: true},
		{noID: 1, epoch: 7, extenders: 6, ctf: -1, extendersConfigured: true},
		{noID: 1, epoch: 8, extenders: 8, ctf: 8, ctfConfigured: true},
		{noID: 1, epoch: 9, extenders: 8, ctf: 9},
		{noID: 1, epoch: 25, extenders: 8, ctf: 9},
		{noID: 2, epoch: 4, extenders: -1, ctf: -1},
		{noID: 2, epoch: 5, extenders: 5, ctf: -1, extendersConfigured: true},
	} {
		in, err := file.At(c.noID, c.epoch, fallback)
		if err != nil {
			t.Fatal(err)
		}
		if fromEpoch(in.Extenders) != c.extenders || fromEpoch(in.CaptureTheFlag) != c.ctf {
			t.Errorf("no %d, epoch %d: extenders from %d, ctf from %d; want %d and %d", c.noID, c.epoch, fromEpoch(in.Extenders), fromEpoch(in.CaptureTheFlag), c.extenders, c.ctf)
			continue
		}
		if in.Extenders != nil && in.Extenders.Configured() != c.extendersConfigured || in.CaptureTheFlag != nil && in.CaptureTheFlag.Configured() != c.ctfConfigured {
			t.Errorf("no %d, epoch %d: a configured entry stood in for a signed one, or the other way", c.noID, c.epoch)
		}
		for _, entry := range []*Entry{in.Extenders, in.CaptureTheFlag} {
			if entry != nil && !entry.Configured() && entry.Verify(testChain, testNetuid, signer) != nil {
				t.Errorf("no %d, epoch %d: an unverified answer", c.noID, c.epoch)
			}
		}
	}
	// A file with no entries, as at launch, leaves configuration alone in force.
	launch := verifiedFile(t, launchFile, signer)
	if in, err := launch.At(1, 8, fallback); err != nil || fromEpoch(in.CaptureTheFlag) != 8 || !in.CaptureTheFlag.Configured() || fromEpoch(in.Extenders) != 6 {
		t.Fatal(in, err)
	}
	if in, err := launch.At(1, 8, nil); err != nil || in.CaptureTheFlag != nil || in.Extenders != nil {
		t.Fatal(in, err)
	}
	if entries := fallback.Entries(); len(entries) != 4 || !entries[0].Configured() || entries[0].Signature != nil {
		t.Fatal(entries)
	}
}

// A deployment with no pinned signer publishes no amendments, so its configuration
// alone puts one in force. One with a pinned signer has no such file: without its
// verified file, what is in force is unknown, whatever its configuration says.
func TestOnlyAnUnpinnedDeploymentIsUnpublished(t *testing.T) {
	if file, err := Unpublished(testChain, testNetuid); err == nil {
		t.Fatalf("SN25 is unpublished: %+v", file)
	}
	const testnet = 945
	unpublished, err := Unpublished(testnet, testNetuid)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := ParseFallback(testnet, testNetuid, []string{strings.Replace(extendersLine(t, 1, 6), "chain 964", "chain 945", 1)})
	if err != nil {
		t.Fatal(err)
	}
	if in, err := unpublished.At(1, 7, fallback); err != nil || fromEpoch(in.Extenders) != 6 || !in.Extenders.Configured() || in.CaptureTheFlag != nil {
		t.Fatal(in, err)
	}
	if in, err := unpublished.At(1, 7, nil); err != nil || in.Extenders != nil || in.CaptureTheFlag != nil {
		t.Fatal(in, err)
	}
	if len(unpublished.Entries()) != 0 || unpublished.Netuid() != testNetuid {
		t.Fatal(unpublished.Entries())
	}
}

func TestParseFallbackRefuses(t *testing.T) {
	other, err := NewExtenders(945, testNetuid, 1, 8).Line()
	if err != nil {
		t.Fatal(err)
	}
	otherSubnet, err := NewExtenders(testChain, 26, 1, 8).Line()
	if err != nil {
		t.Fatal(err)
	}
	for name, lines := range map[string][]string{
		"another chain":       {other},
		"another subnet":      {otherSubnet},
		"out of order":        {extendersLine(t, 1, 9), extendersLine(t, 1, 8)},
		"twice at one epoch":  {ctfLineAt(t, 1, 8), ctfLineAt(t, 1, 8)},
		"a near miss":         {strings.Replace(extendersLine(t, 1, 8), "zeta 1", "zeta 2", 1)},
		"an empty line":       {""},
		"the design's carry":  {strings.Replace(ctfLine, testCarry, "0x<carry contract>", 1)},
		"a line and a broken": {extendersLine(t, 1, 8), "UR settlement amendment v1:"},
	} {
		if _, err := ParseFallback(testChain, testNetuid, lines); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	if fallback, err := ParseFallback(testChain, testNetuid, nil); err != nil || len(fallback.Entries()) != 0 {
		t.Fatal(fallback, err)
	}
	// Kinds and operators interleave freely.
	if _, err := ParseFallback(testChain, testNetuid, []string{extendersLine(t, 2, 10), extendersLine(t, 1, 8), ctfLineAt(t, 1, 8)}); err != nil {
		t.Fatal(err)
	}
}

func TestRefusedFiles(t *testing.T) {
	key, _ := testKey(t)
	good := fileText(t, key, extendersLine(t, 1, 8), ctfLineAt(t, 1, 8))
	if _, err := ParseFile([]byte(good)); err != nil {
		t.Fatal(err)
	}
	line := extendersLine(t, 1, 8)
	replace := func(old, next string) string {
		t.Helper()
		if !strings.Contains(good, old) {
			t.Fatalf("no %q in the file", old)
		}
		return strings.Replace(good, old, next, 1)
	}
	signatureLine := good[strings.Index(good, "    signature: "):]
	signatureLine = signatureLine[:strings.Index(signatureLine, "\n")+1]
	signature := strings.TrimSuffix(strings.TrimPrefix(signatureLine, `    signature: "`), "\"\n")
	for name, file := range map[string]string{
		"not yaml":                "sn: [",
		"empty":                   "",
		"no sn":                   strings.Replace(good, "sn: 25\n", "", 1),
		"sn is text":              replace("sn: 25", "sn: abc"),
		"sn quoted":               replace("sn: 25", `sn: "25"`),
		"sn 025":                  replace("sn: 25", "sn: 025"),
		"sn +25":                  replace("sn: 25", "sn: +25"),
		"sn in hex":               replace("sn: 25", "sn: 0x19"),
		"sn 0":                    replace("sn: 25", "sn: 0"),
		"sn past uint16":          replace("sn: 25", "sn: 65561"),
		"sn repeated":             good + "sn: 25\n",
		"another sn":              replace("sn: 25", "sn: 26"),
		"two documents":           good + "---\nsn: 25\n",
		"a list":                  "- sn: 25\n",
		"entries repeated":        good + "signed_amendments: []\n",
		"entries a mapping":       launchFile + "signed_amendments: {}\n",
		"entries empty":           launchFile + "signed_amendments:\n",
		"entries a string":        launchFile + "signed_amendments: none\n",
		"an entry a list":         launchFile + "signed_amendments:\n  - [a, b]\n",
		"an unknown entry field":  replace(signatureLine, signatureLine+"    note: reviewed\n"),
		"a repeated line field":   replace(signatureLine, signatureLine+`    line: "`+line+"\"\n"),
		"a repeated signature":    replace(signatureLine, signatureLine+signatureLine),
		"no signature":            replace(signatureLine, ""),
		"no line":                 replace(`  - line: "`+line+"\"\n    signature", "  - signature"),
		"a line in single quotes": replace(`"`+line+`"`, `'`+line+`'`),
		"a line with an escape":   replace(`"`+line+`"`, `"`+strings.Replace(line, "zeta 1", `zeta \x31`, 1)+`"`),
		"a line split in two":     replace(`"`+line+`"`, `"`+strings.Replace(line, "free and paid, ", "free and paid,\n      ", 1)+`"`),
		"a line with a tag":       replace(`"`+line+`"`, `!!str "`+line+`"`),
		"a line as an alias":      replace(`"`+line+`"`, `&a "`+line+`"`) + "  - line: *a\n    signature: \"" + signature + "\"\n",
		"a near miss":             replace("zeta 1", "zeta 2"),
		"a signature in caps":     replace(signature, "0x"+strings.ToUpper(signature[2:])),
		"a signature without 0x":  replace(`"`+signature+`"`, `"`+signature[2:]+`"`),
		"a short signature":       replace(signature, signature[:len(signature)-2]),
		"a long signature":        replace(signature, signature+"00"),
		"a signature unquoted":    replace(`"`+signature+`"`, signature),
		"a signature quoted once": replace(`"`+signature+`"`, `'`+signature+`'`),
		"an entry twice": good + `  - line: "` + line + `"
    signature: "` + signature + "\"\n",
		"an earlier entry after a later": fileText(t, key, extendersLine(t, 1, 9), extendersLine(t, 1, 8)),
		"another kind before one listed": fileText(t, key, extendersLine(t, 1, 8), ctfLineAt(t, 1, 5)),
		"another operator before one":    fileText(t, key, extendersLine(t, 1, 8), ctfLineAt(t, 1, 12), extendersLine(t, 2, 3)),
		"a signature with an escape":     replace(`"`+signature+`"`, `"\x30`+signature[1:]+`"`),
		"an entry for another sn": func() string {
			other, err := NewExtenders(testChain, 26, 1, 8).Line()
			if err != nil {
				t.Fatal(err)
			}
			return fileText(t, key, other)
		}(),
	} {
		if parsed, err := ParseFile([]byte(file)); err == nil {
			t.Errorf("%s: parsed %+v", name, parsed)
		}
	}
	if _, err := ParseFile([]byte(good + "#" + strings.Repeat(" ", maxFileBytes) + "\n")); err == nil {
		t.Fatal("parsed an oversized file")
	}
	// What a reader passes over, and what it allows: fields it does not use, the
	// layout of a list, and kinds and operators interleaved in the order they take
	// effect.
	for name, file := range map[string]string{
		"an unused top-level field": good + "notes: kept for people\n",
		"a list at the margin":      strings.ReplaceAll(strings.ReplaceAll(good, "  - line", "- line"), "    signature", "  signature"),
		"kinds and operators interleaved": fileText(t, key,
			extendersLine(t, 1, 8), ctfLineAt(t, 2, 8), ctfLineAt(t, 1, 8), extendersLine(t, 2, 10), ctfLineAt(t, 1, 12)),
		"comments among the entries": strings.Replace(good, "signed_amendments:\n", "signed_amendments:\n  # the extender pool\n", 1),
	} {
		if _, err := ParseFile([]byte(file)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Every entry must verify: one signed by anyone else, or for another deployment, and
// the file is refused.
func TestVerifyChecksEveryEntry(t *testing.T) {
	key, signer := testKey(t)
	attacker, _ := testKey(t)
	good := fileText(t, key, extendersLine(t, 1, 8), ctfLineAt(t, 1, 8))
	verifiedFile(t, good, signer)
	forged := fileText(t, key, extendersLine(t, 1, 8)) + strings.TrimPrefix(fileText(t, attacker, ctfLineAt(t, 1, 8)), launchFile+"signed_amendments:\n")
	file, err := ParseFile([]byte(forged))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Entries()) != 2 {
		t.Fatalf("%+v", file)
	}
	if err := file.Verify(testChain, testNetuid, signer); err == nil {
		t.Fatal("a file with a forged entry verified")
	}
	file, err = ParseFile([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Verify(945, testNetuid, signer); err == nil {
		t.Fatal("verified for another chain")
	}
	if err := file.Verify(testChain, 26, signer); err == nil {
		t.Fatal("verified for another subnet")
	}
}

// A reader takes only entries after the ones it holds. An entry added among them, even
// well signed, would change epochs that reader has already decided, and so would one
// added after them that takes effect before them, which no file can hold.
func TestOnlyALaterEntryExtendsAFile(t *testing.T) {
	key, signer := testKey(t)
	parse := func(lines ...string) *File {
		t.Helper()
		return verifiedFile(t, fileText(t, key, lines...), signer)
	}
	v1, v2, v2Later := extendersLine(t, 1, 8), ctfLineAt(t, 1, 8), ctfLineAt(t, 1, 12)
	last := parse(v1, v2)
	resigned := parse(v1, v2)
	resigned.entries[1].Signature = malleate(resigned.entries[1].Signature)
	for name, c := range map[string]struct {
		next *File
		want bool
	}{
		"the same file":                {parse(v1, v2), true},
		"a later entry":                {parse(v1, v2, v2Later), true},
		"another operator's, as late":  {parse(v1, v2, extendersLine(t, 2, 8)), true},
		"an entry between":             {parse(v1, ctfLineAt(t, 2, 8), v2), false},
		"an entry before all":          {parse(extendersLine(t, 2, 3), v1, v2), false},
		"the newest dropped":           {parse(v1), false},
		"the oldest dropped":           {parse(v2), false},
		"an entry signed once more":    {resigned, false},
		"the entries in other order":   {parse(v2, v1), false},
		"a later one, the newest gone": {parse(v1, v2Later), false},
	} {
		if got := Extends(c.next, last); got != c.want {
			t.Errorf("%s: Extends = %t, want %t", name, got, c.want)
		}
	}
	if !Extends(parse(v1), nil) || !Extends(parse(v1), &File{}) {
		t.Error("a reader holding no entries takes any verified file")
	}
	if Extends(nil, last) {
		t.Error("no file extends one")
	}
	// A file that kept every held entry and added one taking effect before them would
	// change epochs a reader has decided: no file can list it.
	for _, earlier := range []string{ctfLineAt(t, 2, 7), extendersLine(t, 3, 1)} {
		if _, err := ParseFile([]byte(fileText(t, key, v1, v2, earlier))); err == nil {
			t.Fatalf("a file added %q after entries from epoch 8", earlier)
		}
	}
}

// Verify holds a file to the order ParseFile requires, and At answers by first epoch
// however the entries were listed, so neither rests on the other alone.
func TestTheOrderIsCheckedTwice(t *testing.T) {
	key, signer := testKey(t)
	later := signed(t, key, mustParse(t, ctfLineAt(t, 1, 12)))
	earlier := signed(t, key, mustParse(t, ctfLineAt(t, 1, 8)))
	file := &File{netuid: testNetuid, entries: []Entry{*later, *earlier}}
	if err := file.Verify(testChain, testNetuid, signer); err == nil {
		t.Fatal("verified entries out of order")
	}
	file.verified = true
	if in, err := file.At(1, 20, nil); err != nil || fromEpoch(in.CaptureTheFlag) != 12 {
		t.Fatal(in, err)
	}
	if in, err := file.At(1, 10, nil); err != nil || fromEpoch(in.CaptureTheFlag) != 8 {
		t.Fatal(in, err)
	}
	wrongSubnet := signed(t, key, mustParse(t, strings.Replace(ctfLineAt(t, 1, 8), "sn 25", "sn 26", 1)))
	if err := (&File{netuid: testNetuid, entries: []Entry{*wrongSubnet}}).Verify(testChain, testNetuid, signer); err == nil {
		t.Fatal("verified an entry for another subnet")
	}
}

type memoryStore struct {
	raw     []byte
	loadErr error
	saveErr error
	saves   int
}

func (m *memoryStore) Load(context.Context) ([]byte, error) { return m.raw, m.loadErr }

func (m *memoryStore) Save(_ context.Context, raw []byte) error {
	m.saves++
	if m.saveErr != nil {
		return m.saveErr
	}
	m.raw = raw
	return nil
}

// Only a verified file is taken; anything else leaves the last good one in force.
func TestSourceKeepsTheLastGoodFile(t *testing.T) {
	ctx := context.Background()
	key, signer := testKey(t)
	attacker, _ := testKey(t)
	status, body := http.StatusOK, fileText(t, key, extendersLine(t, 1, 8))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cache-Control") != "no-cache" {
			t.Error("a cached copy could answer")
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	store := &memoryStore{}
	source := NewSource(server.URL, testChain, testNetuid, signer, store)
	file, problem := source.Refresh(ctx)
	if problem != nil || len(store.raw) == 0 {
		t.Fatal(file, problem)
	}
	if in, err := file.At(1, 8, nil); err != nil || in.Extenders == nil {
		t.Fatal(in, err)
	}
	stay := func(name string) {
		t.Helper()
		file, problem := source.Refresh(ctx)
		if problem == nil {
			t.Fatal("no problem reported:", name)
		}
		in, err := file.At(1, 100, nil)
		if err != nil || in.Extenders == nil || in.Extenders.FromEpoch != 8 || in.CaptureTheFlag != nil {
			t.Fatalf("%s changed the file in force: %+v %v", name, in, err)
		}
	}
	status = http.StatusInternalServerError
	stay("an outage")
	status, body = http.StatusOK, "<html>not yaml: ["
	stay("an error page")
	body = fileText(t, attacker, extendersLine(t, 1, 8), ctfLineAt(t, 1, 9))
	stay("a forged file")
	body = fileText(t, key, extendersLine(t, 1, 8)) + strings.TrimPrefix(fileText(t, attacker, ctfLineAt(t, 1, 9)), launchFile+"signed_amendments:\n")
	stay("a forged entry after a good one")
	body = launchFile
	stay("a rollback that drops a signed entry")
	body = fileText(t, key, ctfLineAt(t, 1, 9), extendersLine(t, 1, 8))
	stay("a well-signed entry added before one the reader holds")
	body = strings.Replace(fileText(t, key, extendersLine(t, 1, 8)), "sn: 25", "sn: 26", 1)
	stay("a file for another subnet")
	// A newer signed entry is news, and survives a restart.
	body = fileText(t, key, extendersLine(t, 1, 8), ctfLineAt(t, 1, 9))
	file, problem = source.Refresh(ctx)
	if problem != nil {
		t.Fatal(problem)
	}
	if in, err := file.At(1, 9, nil); err != nil || in.CaptureTheFlag == nil {
		t.Fatal(in, err)
	}
	status = http.StatusBadGateway
	restarted := NewSource(server.URL, testChain, testNetuid, signer, store)
	file, problem = restarted.Refresh(ctx)
	if problem == nil || file == nil {
		t.Fatal(file, problem)
	}
	if in, err := file.At(1, 9, nil); err != nil || in.CaptureTheFlag == nil || in.Extenders == nil {
		t.Fatal(in, err)
	}
}

// With nothing ever verified and the file unavailable, the amendments in force are
// unknown rather than assumed, and configuration does not stand in.
func TestSourceWithNothingVerified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	_, signer := testKey(t)
	file, problem := NewSource(server.URL, testChain, testNetuid, signer, &memoryStore{}).Refresh(context.Background())
	if file != nil || problem == nil {
		t.Fatal(file, problem)
	}
	fallback, err := ParseFallback(testChain, testNetuid, []string{extendersLine(t, 1, 8)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.At(1, 8, fallback); !errors.Is(err, ErrUnknown) {
		t.Fatal(err)
	}
}

// A stored copy that is corrupt, or signed by someone else, does not stop the fetch.
// The verified file replaces it, with the stored copy's fault reported once.
func TestACorruptStoredFileIsReplacedByAVerifiedOne(t *testing.T) {
	ctx := context.Background()
	key, signer := testKey(t)
	attacker, _ := testKey(t)
	published := fileText(t, key, extendersLine(t, 1, 8))
	up := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !up {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, published)
	}))
	defer server.Close()
	for name, stored := range map[string]string{
		"not yaml":     "sn: [",
		"forged":       fileText(t, attacker, extendersLine(t, 1, 8)),
		"another sn":   strings.Replace(launchFile, "sn: 25", "sn: 26", 1),
		"another type": "{}",
	} {
		store := &memoryStore{raw: []byte(stored)}
		source := NewSource(server.URL, testChain, testNetuid, signer, store)
		up = false
		if file, problem := source.Refresh(ctx); file != nil || problem == nil {
			t.Fatalf("%s, site down: %v %v", name, file, problem)
		}
		up = true
		file, problem := source.Refresh(ctx)
		if file == nil || problem == nil || !strings.Contains(problem.Error(), "stored file") {
			t.Fatalf("%s: %v %v", name, file, problem)
		}
		if string(store.raw) != published {
			t.Fatalf("%s: the corrupt copy was not replaced", name)
		}
		if file, problem = source.Refresh(ctx); file == nil || problem != nil {
			t.Fatalf("%s, after the repair: %v %v", name, file, problem)
		}
	}
}

// A store that cannot be read may still hold a good copy. The verified file is used
// this time, but nothing overwrites the copy the store may still hold.
func TestAnUnreadableStoreIsNotOverwritten(t *testing.T) {
	key, signer := testKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, fileText(t, key, extendersLine(t, 1, 8)))
	}))
	defer server.Close()
	store := &memoryStore{raw: []byte("held"), loadErr: errors.New("connection reset")}
	file, problem := NewSource(server.URL, testChain, testNetuid, signer, store).Refresh(context.Background())
	if file == nil || problem == nil || !strings.Contains(problem.Error(), "connection reset") {
		t.Fatal(file, problem)
	}
	if store.saves != 0 || string(store.raw) != "held" {
		t.Fatalf("saved %d times over an unreadable store: %q", store.saves, store.raw)
	}
}

// A redirect leaves the published address and is not followed.
func TestSourceDoesNotFollowRedirects(t *testing.T) {
	key, signer := testKey(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, fileText(t, key, extendersLine(t, 1, 8)))
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer server.Close()
	file, problem := NewSource(server.URL, testChain, testNetuid, signer, nil).Refresh(context.Background())
	if file != nil || problem == nil {
		t.Fatal(file, problem)
	}
}

func TestSourceRefusesAnOversizedFile(t *testing.T) {
	key, signer := testKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, fileText(t, key, extendersLine(t, 1, 8))+"#"+strings.Repeat(" ", maxFileBytes)+"\n")
	}))
	defer server.Close()
	file, problem := NewSource(server.URL, testChain, testNetuid, signer, nil).Refresh(context.Background())
	if file != nil || problem == nil {
		t.Fatal(file, problem)
	}
}

func TestSaveFailureStillUsesTheVerifiedFile(t *testing.T) {
	key, signer := testKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, fileText(t, key, extendersLine(t, 1, 8)))
	}))
	defer server.Close()
	store := &memoryStore{saveErr: errors.New("disk full")}
	file, problem := NewSource(server.URL, testChain, testNetuid, signer, store).Refresh(context.Background())
	if file == nil || problem == nil {
		t.Fatal(file, problem)
	}
	if in, err := file.At(1, 8, nil); err != nil || in.Extenders == nil {
		t.Fatal(in, err)
	}
}

// servedFiles answers each request with the next of bodies, then the last again.
func servedFiles(t *testing.T, bodies ...func() (int, string)) *httptest.Server {
	t.Helper()
	var lock sync.Mutex
	next := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		lock.Lock()
		body := bodies[min(next, len(bodies)-1)]
		next++
		lock.Unlock()
		status, text := body()
		w.WriteHeader(status)
		fmt.Fprint(w, text)
	}))
	t.Cleanup(server.Close)
	return server
}

func ok(text string) func() (int, string) {
	return func() (int, string) { return http.StatusOK, text }
}

func down() (int, string) { return http.StatusBadGateway, "" }

// A source holds the last good file itself: with no store, or one it cannot read, a
// rollback is refused and an outage keeps the file in force.
func TestASourceHoldsItsOwnLastGoodFile(t *testing.T) {
	ctx := context.Background()
	key, signer := testKey(t)
	two := fileText(t, key, extendersLine(t, 1, 8), ctfLineAt(t, 1, 8))
	one := fileText(t, key, extendersLine(t, 1, 8))
	for name, store := range map[string]Store{
		"no store":            nil,
		"an unreadable store": &memoryStore{loadErr: errors.New("connection reset")},
	} {
		server := servedFiles(t, ok(two), ok(one), down, ok(launchFile))
		source := NewSource(server.URL, testChain, testNetuid, signer, store)
		file, problem := source.Refresh(ctx)
		if file == nil || len(file.Entries()) != 2 || store == nil && problem != nil {
			t.Fatalf("%s: %v %v", name, file, problem)
		}
		for _, what := range []string{"a rollback", "an outage", "the launch file again"} {
			file, problem := source.Refresh(ctx)
			if problem == nil || file == nil || len(file.Entries()) != 2 {
				t.Fatalf("%s, %s: %v %v", name, what, file, problem)
			}
			if in, err := file.At(1, 8, nil); err != nil || in.CaptureTheFlag == nil {
				t.Fatalf("%s, %s: %v %v", name, what, in, err)
			}
		}
	}
}

// Two readers share a store, as the server's replicas do. One that read the store
// before the other saved a newer file, and then fetched an older one from a stale
// cache, does not save it over the newer.
func TestASharedStoreIsNotWalkedBack(t *testing.T) {
	ctx := context.Background()
	key, signer := testKey(t)
	one := fileText(t, key, extendersLine(t, 1, 8))
	two := fileText(t, key, extendersLine(t, 1, 8), ctfLineAt(t, 1, 8))
	store := &memoryStore{raw: []byte(one)}
	arrived, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stale" {
			close(arrived)
			<-release
			fmt.Fprint(w, one)
			return
		}
		fmt.Fprint(w, two)
	}))
	defer server.Close()
	slow := NewSource(server.URL+"/stale", testChain, testNetuid, signer, store)
	fast := NewSource(server.URL+"/fresh", testChain, testNetuid, signer, store)
	done := make(chan struct{})
	var slowFile *File
	go func() {
		defer close(done)
		slowFile, _ = slow.Refresh(ctx)
	}()
	<-arrived
	if file, problem := fast.Refresh(ctx); problem != nil || len(file.Entries()) != 2 || string(store.raw) != two {
		t.Fatal(file, problem, string(store.raw))
	}
	close(release)
	<-done
	if string(store.raw) != two {
		t.Fatalf("the newer stored file was walked back:\n%s", store.raw)
	}
	if slowFile == nil || len(slowFile.Entries()) != 1 {
		t.Fatal(slowFile)
	}
	// And the slow reader's next refresh starts from the newer stored file.
	if file, problem := NewSource(server.URL+"/fresh", testChain, testNetuid, signer, store).Refresh(ctx); problem != nil || len(file.Entries()) != 2 {
		t.Fatal(file, problem)
	}
}

// Refreshes run one at a time, so a stale answer that arrives late never replaces the
// newer file one source holds or stores.
func TestConcurrentRefreshesKeepTheNewest(t *testing.T) {
	ctx := context.Background()
	key, signer := testKey(t)
	one := fileText(t, key, extendersLine(t, 1, 8))
	two := fileText(t, key, extendersLine(t, 1, 8), ctfLineAt(t, 1, 8))
	var lock sync.Mutex
	served := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		lock.Lock()
		served++
		body := one
		if served%2 == 0 {
			body = two
		}
		lock.Unlock()
		fmt.Fprint(w, body)
	}))
	defer server.Close()
	store := &lockedStore{}
	source := NewSource(server.URL, testChain, testNetuid, signer, store)
	var group sync.WaitGroup
	for range 16 {
		group.Add(1)
		go func() {
			defer group.Done()
			if file, _ := source.Refresh(ctx); file == nil {
				t.Error("a refresh returned no file")
			}
		}()
	}
	group.Wait()
	file, _ := source.Refresh(ctx)
	if file == nil || len(file.Entries()) != 2 || string(store.load()) != two {
		t.Fatalf("%v\n%s", file, store.load())
	}
}

type lockedStore struct {
	lock sync.Mutex
	raw  []byte
}

func (l *lockedStore) load() []byte {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.raw
}

func (l *lockedStore) Load(context.Context) ([]byte, error) { return l.load(), nil }

func (l *lockedStore) Save(_ context.Context, raw []byte) error {
	l.lock.Lock()
	defer l.lock.Unlock()
	l.raw = raw
	return nil
}
