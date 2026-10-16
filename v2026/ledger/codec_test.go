package ledger

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/holiman/uint256"

	"github.com/urfoundation/sn/v2026/merkle"
	"github.com/urfoundation/sn/v2026/protocol"
)

func testHeader() *Header {
	return &Header{
		NoID: 7, Epoch: 12,
		Epochs: []HeaderEpoch{
			{Epoch: 10, Null: true, Providers: []Ref{}},
			{Epoch: 11, Providers: []Ref{}},
			{Epoch: 12, Providers: []Ref{RefOf([]byte("a")), RefOf([]byte("b"))}},
		},
		Accounts: []Ref{RefOf([]byte("c"))},
	}
}

func testHeaderText() string {
	return fmt.Sprintf(`{"schema":"urnetwork-payout-ledger-v2","no_id":7,"epoch":12,"epochs":[{"epoch":10,"providers":null},{"epoch":11,"providers":[]},{"epoch":12,"providers":["%v","%v"]}],"accounts":["%v"]}`,
		RefOf([]byte("a")), RefOf([]byte("b")), RefOf([]byte("c")))
}

// GL-1: a header encodes to exactly the canonical JSON of §6.5 and decodes
// back to itself.
func TestHeaderRoundTrip(t *testing.T) {
	h := testHeader()
	data, err := EncodeHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != testHeaderText() {
		t.Fatalf("header\n%s\nwant\n%s", data, testHeaderText())
	}
	decoded, err := DecodeHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, h) {
		t.Fatalf("decoded %+v, want %+v", decoded, h)
	}
	// Encoding ignores the providers of an unrecoverable epoch.
	h.Epochs[0].Providers = []Ref{RefOf(nil)}
	if again, err := EncodeHeader(h); err != nil || !bytes.Equal(again, data) {
		t.Fatalf("null epoch with providers encodes %s, %v", again, err)
	}
	// An empty account list and an epoch without rows are []; null is only
	// ever written for an unrecoverable epoch.
	empty, err := EncodeHeader(&Header{NoID: 1, Epoch: 1, Epochs: []HeaderEpoch{{Epoch: 1}}})
	if err != nil || string(empty) != `{"schema":"urnetwork-payout-ledger-v2","no_id":1,"epoch":1,"epochs":[{"epoch":1,"providers":[]}],"accounts":[]}` {
		t.Fatalf("empty header %s, %v", empty, err)
	}
	if _, err := DecodeHeader(empty); err != nil {
		t.Fatal(err)
	}
}

// GL-1: every refusal of the canonical form, each by one change to a
// canonical header.
func TestHeaderRefusals(t *testing.T) {
	canonical := testHeaderText()
	a, b := RefOf([]byte("a")).String(), RefOf([]byte("b")).String()
	cases := []struct {
		name   string
		header string
		want   error
	}{
		{"spacing after a colon", strings.Replace(canonical, `"no_id":7`, `"no_id": 7`, 1), ErrHeaderNotCanonical},
		{"spacing after a comma", strings.Replace(canonical, `,"epoch":12`, `, "epoch":12`, 1), ErrHeaderNotCanonical},
		{"leading space", " " + canonical, ErrHeaderNotCanonical},
		{"key order", strings.Replace(canonical, `"no_id":7,"epoch":12`, `"epoch":12,"no_id":7`, 1), ErrHeaderNotCanonical},
		{"key case", strings.Replace(canonical, `"no_id"`, `"No_id"`, 1), ErrHeaderNotCanonical},
		{"repeated key", strings.Replace(canonical, `"no_id":7`, `"no_id":7,"no_id":7`, 1), ErrHeaderNotCanonical},
		{"escaped string", strings.Replace(canonical, `"urnetwork`, `"\u0075rnetwork`, 1), ErrHeaderNotCanonical},
		{"hex case", strings.Replace(canonical, a, "sha256:"+strings.ToUpper(a[7:]), 1), ErrRef},
		{"missing sha256: prefix", strings.Replace(canonical, a, a[7:], 1), ErrRef},
		{"other prefix", strings.Replace(canonical, a, "sha512:"+a[7:], 1), ErrRef},
		{"prefix case", strings.Replace(canonical, a, "SHA256:"+a[7:], 1), ErrRef},
		{"short reference", strings.Replace(canonical, a, a[:70], 1), ErrRef},
		{"long reference", strings.Replace(canonical, a, a+"0", 1), ErrRef},
		{"non-hex digit", strings.Replace(canonical, a, a[:70]+"g", 1), ErrRef},
		{"account reference", strings.Replace(canonical, RefOf([]byte("c")).String(), RefOf([]byte("c")).String()[7:], 1), ErrRef},
		{"unknown field", strings.Replace(canonical, `"no_id":7`, `"no_id":7,"extra":1`, 1), ErrHeaderDecode},
		{"unknown epoch field", strings.Replace(canonical, `{"epoch":11,`, `{"epoch":11,"rows":0,`, 1), ErrHeaderDecode},
		{"trailing newline", canonical + "\n", ErrHeaderTrailingData},
		{"trailing value", canonical + "{}", ErrHeaderTrailingData},
		{"null epochs", canonical[:strings.Index(canonical, `"epochs":`)] + `"epochs":null,"accounts":["` + RefOf([]byte("c")).String() + `"]}`, ErrHeaderNull},
		{"null accounts", canonical[:strings.Index(canonical, `"accounts":`)] + `"accounts":null}`, ErrHeaderNull},
		{"null no_id", strings.Replace(canonical, `"no_id":7`, `"no_id":null`, 1), ErrHeaderNotCanonical},
		{"null schema", strings.Replace(canonical, `"urnetwork-payout-ledger-v2"`, `null`, 1), ErrHeaderNotCanonical},
		{"null epoch entry", strings.Replace(canonical, `{"epoch":10,"providers":null}`, `null`, 1), ErrHeaderNotCanonical},
		{"null reference", strings.Replace(canonical, `"`+b+`"`, `null`, 1), ErrHeaderNotCanonical},
		{"missing key", strings.Replace(canonical, `"no_id":7,`, ``, 1), ErrHeaderNotCanonical},
		{"missing accounts", canonical[:strings.Index(canonical, `,"accounts":`)] + `}`, ErrHeaderNotCanonical},
		{"missing providers", strings.Replace(canonical, `{"epoch":11,"providers":[]}`, `{"epoch":11}`, 1), ErrHeaderNotCanonical},
		{"integer as decimal", strings.Replace(canonical, `"no_id":7`, `"no_id":7.0`, 1), ErrHeaderDecode},
		{"integer with exponent", strings.Replace(canonical, `"no_id":7`, `"no_id":7e0`, 1), ErrHeaderDecode},
		{"negative integer", strings.Replace(canonical, `"no_id":7`, `"no_id":-7`, 1), ErrHeaderDecode},
		{"integer as string", strings.Replace(canonical, `"no_id":7`, `"no_id":"7"`, 1), ErrHeaderDecode},
		{"leading zero", strings.Replace(canonical, `"no_id":7`, `"no_id":07`, 1), ErrHeaderDecode},
		{"integer of 2^64", strings.Replace(canonical, `"no_id":7`, `"no_id":18446744073709551616`, 1), ErrHeaderDecode},
		{"not JSON", canonical[:20], ErrHeaderDecode},
		{"other schema", strings.Replace(canonical, `-v2"`, `-v1"`, 1), ErrHeaderSchema},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := DecodeHeader([]byte(c.header)); !errors.Is(err, c.want) {
				t.Fatalf("%s: %v, want %v", c.header, err, c.want)
			}
		})
	}
}

// headerOfSize returns a canonical header of exactly size bytes, whether
// or not EncodeHeader would accept it.
func headerOfSize(t *testing.T, size int) []byte {
	t.Helper()
	encode := func(n, epochs, noDigits, epochDigits int) []byte {
		noID, _ := strconv.ParseUint(strings.Repeat("1", noDigits), 10, 64)
		epoch, _ := strconv.ParseUint(strings.Repeat("2", epochDigits), 10, 64)
		raw := headerJSON{Schema: Schema, NoID: noID, Epoch: epoch, Accounts: slices.Repeat([]string{RefOf(nil).String()}, n)}
		for range epochs {
			raw.Epochs = append(raw.Epochs, headerEpochJSON{Epoch: epoch, Providers: []string{}})
		}
		return raw.appendJSON(nil)
	}
	// Each account reference after the first adds 74 bytes; the epoch
	// entries and the digits of no_id and epoch make up the rest.
	for epochs := 1; epochs < 5; epochs++ {
		for noDigits := 1; noDigits < 20; noDigits++ {
			for epochDigits := 1; epochDigits < 20; epochDigits++ {
				if rest := size - len(encode(0, epochs, noDigits, epochDigits)) + 1; rest > 0 && rest%74 == 0 {
					return encode(rest/74, epochs, noDigits, epochDigits)
				}
			}
		}
	}
	t.Fatalf("no header of %d bytes", size)
	return nil
}

// GL-1: a header of exactly 256 KiB is accepted, one byte more is refused,
// by the decoder and by the encoder.
func TestHeaderSizeBound(t *testing.T) {
	data := headerOfSize(t, MaxHeaderBytes)
	h, err := DecodeHeader(data)
	if err != nil {
		t.Fatalf("256 KiB header refused: %v", err)
	}
	if again, err := EncodeHeader(h); err != nil || !bytes.Equal(again, data) {
		t.Fatalf("256 KiB header re-encodes: %v", err)
	}
	over := headerOfSize(t, MaxHeaderBytes+1)
	if _, err := DecodeHeader(over); !errors.Is(err, ErrHeaderTooLarge) {
		t.Fatalf("257 KiB header: %v", err)
	}
	h.Accounts = append(h.Accounts, RefOf(nil))
	if _, err := EncodeHeader(h); !errors.Is(err, ErrHeaderTooLarge) {
		t.Fatalf("encoding an oversized header: %v", err)
	}
}

// GL-1: 96 epochs are accepted, 97 refused, by the decoder and by the encoder.
func TestHeaderEpochBound(t *testing.T) {
	h := &Header{NoID: 1, Epoch: 96, Accounts: []Ref{}}
	for k := uint64(1); k <= MaxCoveredEpochs; k++ {
		h.Epochs = append(h.Epochs, HeaderEpoch{Epoch: k, Providers: []Ref{}})
	}
	data, err := EncodeHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeHeader(data); err != nil {
		t.Fatalf("96 epochs refused: %v", err)
	}
	more := bytes.Replace(data, []byte(`"epochs":[`), []byte(`"epochs":[{"epoch":0,"providers":[]},`), 1)
	if _, err := DecodeHeader(more); !errors.Is(err, ErrTooManyEpochs) {
		t.Fatalf("97 epochs: %v", err)
	}
	h.Epochs = append(h.Epochs, HeaderEpoch{Epoch: 97})
	if _, err := EncodeHeader(h); !errors.Is(err, ErrTooManyEpochs) {
		t.Fatalf("encoding 97 epochs: %v", err)
	}
}

// GL-1: references.
func TestRef(t *testing.T) {
	ref := RefOf([]byte("abc"))
	text := ref.String()
	if text != "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("reference %s", text)
	}
	parsed, err := ParseRef(text)
	if err != nil || parsed != ref {
		t.Fatalf("parsed %v, %v", parsed, err)
	}
	for _, bad := range []string{"", text[7:], "sha256:" + strings.ToUpper(text[7:]), text[:70], text + "0", "sha256:" + strings.Repeat("x", 64), "sha256 " + text[7:]} {
		if _, err := ParseRef(bad); !errors.Is(err, ErrRef) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}

// GL-1: provider rows are 57 bytes, big-endian, in chunks of 131,072 rows
// with a non-empty last chunk.
func TestProviderChunks(t *testing.T) {
	rows := []protocol.LedgerUsageRow{
		{ClientID: id(1), Coldkey: key(9), UsageBytes: 0x0102030405060708, HeadExcluded: true},
		{ClientID: id(2), UsageBytes: 1},
	}
	chunks := EncodeProviderChunks(rows)
	if len(chunks) != 1 || len(chunks[0]) != 2*ProviderRowBytes {
		t.Fatalf("chunks %d", len(chunks))
	}
	client, coldkey := id(1), key(9)
	want := append(append(append(client[:], coldkey[:]...), 1, 2, 3, 4, 5, 6, 7, 8), 1)
	if !bytes.Equal(chunks[0][:ProviderRowBytes], want) {
		t.Fatalf("row %x, want %x", chunks[0][:ProviderRowBytes], want)
	}
	if chunks[0][2*ProviderRowBytes-1] != 0 {
		t.Fatal("head_excluded 0 not encoded")
	}
	decoded, err := DecodeProviderChunks(chunks)
	if err != nil || !reflect.DeepEqual(decoded, rows) {
		t.Fatalf("decoded %+v, %v", decoded, err)
	}
	if chunks := EncodeProviderChunks(nil); len(chunks) != 0 {
		t.Fatalf("no rows give %d chunks", len(chunks))
	}
	many := make([]protocol.LedgerUsageRow, ChunkRows+1)
	for i := range many {
		many[i] = row(i+1, 1, 1, false)
	}
	chunks = EncodeProviderChunks(many)
	if len(chunks) != 2 || len(chunks[0]) != ChunkRows*ProviderRowBytes || len(chunks[1]) != ProviderRowBytes {
		t.Fatalf("%d rows give chunks of %d and %d bytes", len(many), len(chunks[0]), len(chunks[1]))
	}
	if decoded, err := DecodeProviderChunks(chunks); err != nil || !reflect.DeepEqual(decoded, many) {
		t.Fatalf("decoding two chunks: %v", err)
	}
	if chunks := EncodeProviderChunks(many[:ChunkRows]); len(chunks) != 1 {
		t.Fatalf("%d rows give %d chunks", ChunkRows, len(chunks))
	}
}

// GL-1: account rows are 48 bytes; a cumulative must be below 2^128.
func TestAccountChunks(t *testing.T) {
	accounts := []merkle.LedgerAccount{{Coldkey: key(1)}, {Coldkey: key(2)}}
	accounts[0].Cumulative.SetUint64(0x0102)
	accounts[1].Cumulative.Sub(valueLimit, uint256.NewInt(1))
	chunks, err := EncodeAccountChunks(accounts)
	if err != nil || len(chunks) != 1 || len(chunks[0]) != 2*AccountRowBytes {
		t.Fatalf("chunks %d, %v", len(chunks), err)
	}
	coldkey := key(1)
	want := append(coldkey[:], make([]byte, 16)...)
	want[46], want[47] = 1, 2
	if !bytes.Equal(chunks[0][:AccountRowBytes], want) {
		t.Fatalf("row %x, want %x", chunks[0][:AccountRowBytes], want)
	}
	decoded, err := DecodeAccountChunks(chunks)
	if err != nil || !reflect.DeepEqual(decoded, accounts) {
		t.Fatalf("decoded %+v, %v", decoded, err)
	}
	accounts[1].Cumulative.Set(valueLimit)
	if _, err := EncodeAccountChunks(accounts); !errors.Is(err, merkle.ErrLedgerValueRange) {
		t.Fatalf("2^128: %v", err)
	}
	many := make([]merkle.LedgerAccount, ChunkRows+1)
	chunks, err = EncodeAccountChunks(many)
	if err != nil || len(chunks) != 2 || len(chunks[1]) != AccountRowBytes {
		t.Fatalf("%d accounts give %d chunks, %v", len(many), len(chunks), err)
	}
	if decoded, err := DecodeAccountChunks(chunks); err != nil || len(decoded) != len(many) {
		t.Fatalf("decoding two chunks: %v", err)
	}
}

// GL-9 (check 4), by the codec: a short chunk before the last, an empty
// last chunk, a length that is not whole rows or exceeds 131,072 rows, and
// a head_excluded byte other than 0 or 1.
func TestChunkRefusals(t *testing.T) {
	full := make([]byte, ChunkRows*ProviderRowBytes)
	one := make([]byte, ProviderRowBytes)
	cases := []struct {
		name   string
		chunks [][]byte
		want   error
	}{
		{"short middle chunk", [][]byte{one, one}, ErrChunkShort},
		{"empty middle chunk", [][]byte{{}, one}, ErrChunkShort},
		{"empty last chunk", [][]byte{full, {}}, ErrChunkEmpty},
		{"empty only chunk", [][]byte{{}}, ErrChunkEmpty},
		{"bad row length", [][]byte{one[:ProviderRowBytes-1]}, ErrChunkLength},
		{"too many rows", [][]byte{append(full, one...)}, ErrChunkLength},
		{"head_excluded 2", [][]byte{append(make([]byte, ProviderRowBytes-1), 2)}, ErrRowEncoding},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := DecodeProviderChunks(c.chunks); !errors.Is(err, c.want) {
				t.Fatalf("%v, want %v", err, c.want)
			}
		})
	}
	if _, err := DecodeAccountChunks([][]byte{make([]byte, AccountRowBytes+1)}); !errors.Is(err, ErrChunkLength) {
		t.Fatalf("account chunk length: %v", err)
	}
	if _, err := DecodeAccountChunks([][]byte{make([]byte, AccountRowBytes), make([]byte, AccountRowBytes)}); !errors.Is(err, ErrChunkShort) {
		t.Fatalf("short account chunk: %v", err)
	}
}
