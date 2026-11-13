package usage

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/protocol"
)

// A fixed test key, so a sealed statement's bytes are fixed too:
// go-ethereum signs with a deterministic nonce.
const goldenKey = "4f3edf983ac636a65a842ce7c78d9aa706d3b113bce9c46f30d7d21715b23b1d"

// The statement sample() makes, sealed with goldenKey by the package before
// version 2 existed (sn cbc025ad). A version 1 statement must keep exactly
// these bytes.
const goldenV1 = `{"schema":"urnetwork-epoch-usage-v1","deployment_id":"sn25-mainnet-test","chain_id":964,"genesis_hash":"0x01","netuid":25,"coordinator":"0x0000000000000000000000000000000000000001","vault_v2":"0x0000000000000000000000000000000000000002","policy_hash":"0x03","no_id":1,"epoch":4,"start":{"number":100,"hash":"0xa"},"end":{"number":200,"hash":"0xb"},"provider_chunks":["sha256:aa"],"provider_rows":2,"total_usage_bytes":30,"total_users":7,"signer":"0x90f8bf6a479f320ead074411a4b0e7944ea8c9c1","signature":"0x748542072b0aeebda5abb4459814ee70c006542e3d2bb546ac105e49494d93c53ac4f0a6223d872071bcfa03bcaa9a5cd94f52c863f53b83249a1111a2deae1a01"}`

// Seals statements with goldenKey and returns their bytes.
func goldenSeal(t *testing.T) func(*Statement) []byte {
	t.Helper()
	key, err := crypto.HexToECDSA(goldenKey)
	if err != nil {
		t.Fatal(err)
	}
	return func(statement *Statement) []byte {
		t.Helper()
		if err := statement.Seal(key); err != nil {
			t.Fatal(err)
		}
		raw, err := statement.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
}

// The text of the test reference whose bytes are b followed by zeros.
func ref(b byte) string {
	return ledger.Ref{b}.String()
}

// The statement sample() makes, for an epoch with an extender component: the
// worked example's Z of 1,000,000,000,001 bytes (WHITEPAPER.md §8.6.5), below
// the cap, so Z' = Z, with N = 3 eligible extenders in M = 2 units.
func sampleV2() *Statement {
	statement := sample()
	statement.Epoch = 8
	statement.TotalUsageBytes = 30 + 1_000_000_000_001
	statement.ExtenderPool = &ExtenderPool{
		IntraPlatformBytes: 600_000_000_001, IntraNetworkBytes: 400_000_000_000, ExtenderPoolBytes: 1_000_000_000_001,
		ExtenderPoolWeightBytes: 1_000_000_000_001, ExtenderPoolContracts: 12, ExtenderPoolPayers: 3,
		ExtenderCensus: []string{ref(1)}, ExtenderUnits: 2, EligibleExtenders: 3, ExtenderChunks: []string{ref(2)},
	}
	return statement
}

// A version 1 statement is sealed to exactly the bytes it was sealed to before
// version 2 existed, so every epoch before E_x publishes what it always would.
func TestVersion1BytesUnchanged(t *testing.T) {
	seal := goldenSeal(t)
	statement := sample()
	if raw := seal(statement); string(raw) != goldenV1 {
		t.Fatalf("a version 1 statement now seals to\n%s\nnot\n%s", raw, goldenV1)
	}
	if statement.Kind() != EvidenceKind || KindOf(false) != EvidenceKind {
		t.Fatalf("a version 1 statement is published as %q", statement.Kind())
	}
	decoded, err := Decode([]byte(goldenV1))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ExtenderPool != nil {
		t.Fatal("a version 1 statement decoded with an extender pool")
	}
	if err := decoded.Verify(statement.Signer); err != nil {
		t.Fatal(err)
	}
	if again, _ := decoded.Bytes(); string(again) != goldenV1 {
		t.Fatalf("a decoded version 1 statement re-encodes to %s", again)
	}
}

// A version 2 statement seals under its own schema and kind, writes its
// extender pool between total_users and signer, verifies, and decodes to what
// was sealed.
func TestVersion2SealVerifyAndRoundTrip(t *testing.T) {
	seal := goldenSeal(t)
	statement := sampleV2()
	raw := seal(statement)
	if statement.Schema != SchemaV2 || statement.Kind() != EvidenceKindV2 || KindOf(true) != EvidenceKindV2 {
		t.Fatalf("a version 2 statement is %q, published as %q", statement.Schema, statement.Kind())
	}
	// The extender pool is written between total_users and signer, in its order.
	keys, err := objectKeys(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"schema", "deployment_id", "chain_id", "genesis_hash", "netuid", "coordinator", "vault_v2", "policy_hash", "no_id", "epoch", "start", "end",
		"provider_chunks", "provider_rows", "total_usage_bytes", "total_users",
		"intra_platform_bytes", "intra_network_bytes", "extender_pool_bytes", "extender_pool_weight_bytes", "extender_pool_contracts", "extender_pool_payers",
		"extender_census", "extender_units", "eligible_extenders", "extender_chunks", "signer", "signature"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("a version 2 statement writes %v", keys)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := back.Verify(statement.Signer); err != nil {
		t.Fatal(err)
	}
	if again, _ := back.Bytes(); string(again) != string(raw) {
		t.Fatal("a decoded version 2 statement re-encodes differently")
	}
	if back.ExtenderPoolBytes != 1_000_000_000_001 || back.EligibleExtenders != 3 || back.ExtenderUnits != 2 {
		t.Fatalf("%+v", back.ExtenderPool)
	}
	if again := seal(sampleV2()); string(again) != string(raw) {
		t.Fatal("sealing the same version 2 statement twice differs")
	}
	// Every field is signed: changing any of them breaks the signature.
	for name, change := range map[string]func(*ExtenderPool){
		"intra platform": func(p *ExtenderPool) { p.IntraPlatformBytes--; p.IntraNetworkBytes++ },
		"pool bytes":     func(p *ExtenderPool) { p.IntraNetworkBytes++; p.ExtenderPoolBytes++ },
		"weight":         func(p *ExtenderPool) { p.ExtenderPoolWeightBytes-- },
		"contracts":      func(p *ExtenderPool) { p.ExtenderPoolContracts++ },
		"payers":         func(p *ExtenderPool) { p.ExtenderPoolPayers-- },
		"census":         func(p *ExtenderPool) { p.ExtenderCensus = []string{ref(3)} },
		"units":          func(p *ExtenderPool) { p.ExtenderUnits = 3 },
		"eligible":       func(p *ExtenderPool) { p.EligibleExtenders = 4 },
		"chunks":         func(p *ExtenderPool) { p.ExtenderChunks = []string{ref(4)} },
	} {
		forged := *back
		pool := *back.ExtenderPool
		forged.ExtenderPool = &pool
		change(&pool)
		if err := forged.Verify(statement.Signer); err == nil {
			t.Fatal("a changed version 2 statement verified:", name)
		}
	}
	// Dropping the extender pool is a version 1 statement under a version 2 schema.
	stripped := *back
	stripped.ExtenderPool = nil
	if err := stripped.Verify(statement.Signer); err == nil {
		t.Fatal("a version 2 statement without its extender pool verified")
	}
}

// A sealed version 2 statement does not decode when its schema names the other
// version, a field breaks the extender pool's relations, a list is null or
// missing, a reference does not parse, or an unknown field appears.
func TestVersion2DecodeRefusals(t *testing.T) {
	seal := goldenSeal(t)
	raw := seal(sampleV2())
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	edit := func(change func(map[string]json.RawMessage)) []byte {
		copied := map[string]json.RawMessage{}
		for k, v := range fields {
			copied[k] = v
		}
		change(copied)
		out, err := json.Marshal(copied)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	v1 := []byte(goldenV1)
	var v1Fields map[string]json.RawMessage
	if err := json.Unmarshal(v1, &v1Fields); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"v1 schema with the extender pool": edit(func(f map[string]json.RawMessage) { f["schema"] = json.RawMessage(`"` + Schema + `"`) }),
		"v2 schema without the extender pool": func() []byte {
			copied := map[string]json.RawMessage{}
			for k, v := range v1Fields {
				copied[k] = v
			}
			copied["schema"] = json.RawMessage(`"` + SchemaV2 + `"`)
			out, _ := json.Marshal(copied)
			return out
		}(),
		"one extender field on a v1 statement": func() []byte {
			copied := map[string]json.RawMessage{}
			for k, v := range v1Fields {
				copied[k] = v
			}
			copied["extender_pool_bytes"] = json.RawMessage(`0`)
			out, _ := json.Marshal(copied)
			return out
		}(),
		"null census":      edit(func(f map[string]json.RawMessage) { f["extender_census"] = json.RawMessage(`null`) }),
		"missing chunks":   edit(func(f map[string]json.RawMessage) { delete(f, "extender_chunks") }),
		"a bad reference":  edit(func(f map[string]json.RawMessage) { f["extender_census"] = json.RawMessage(`["sha256:aa"]`) }),
		"parts not Z":      edit(func(f map[string]json.RawMessage) { f["intra_network_bytes"] = json.RawMessage(`400000000001`) }),
		"weight above Z":   edit(func(f map[string]json.RawMessage) { f["extender_pool_weight_bytes"] = json.RawMessage(`1000000000002`) }),
		"more units":       edit(func(f map[string]json.RawMessage) { f["extender_units"] = json.RawMessage(`4`) }),
		"a unit, none":     edit(func(f map[string]json.RawMessage) { f["eligible_extenders"] = json.RawMessage(`0`) }),
		"no contracts":     edit(func(f map[string]json.RawMessage) { f["extender_pool_contracts"] = json.RawMessage(`0`) }),
		"more payers":      edit(func(f map[string]json.RawMessage) { f["extender_pool_payers"] = json.RawMessage(`13`) }),
		"rows, no weight":  edit(func(f map[string]json.RawMessage) { f["extender_pool_weight_bytes"] = json.RawMessage(`0`) }),
		"weight, no rows":  edit(func(f map[string]json.RawMessage) { f["extender_chunks"] = json.RawMessage(`[]`) }),
		"total without Z":  edit(func(f map[string]json.RawMessage) { f["total_usage_bytes"] = json.RawMessage(`30`) }),
		"an unknown field": edit(func(f map[string]json.RawMessage) { f["extender_surprise"] = json.RawMessage(`1`) }),
		"v2 at v1's kind":  []byte(strings.Replace(string(raw), SchemaV2, Schema, 1)),
		"v1 named as v2":   []byte(strings.Replace(goldenV1, Schema, SchemaV2, 1)),
		"weight, none elig": edit(func(f map[string]json.RawMessage) {
			f["eligible_extenders"] = json.RawMessage(`0`)
			f["extender_units"] = json.RawMessage(`0`)
		}),
	} {
		if _, err := Decode(data); err == nil {
			t.Fatal("decoded", name)
		}
	}
}

// A version 2 statement's bytes are its provider rows' usage and all of Z, before
// the cap.
func TestVersion2Rows(t *testing.T) {
	rows := []protocol.LedgerUsageRow{{UsageBytes: 10}, {UsageBytes: 20}}
	statement := sampleV2()
	if err := statement.CheckRows(rows); err != nil {
		t.Fatal(err)
	}
	if err := statement.CheckSums(30, 2); err != nil {
		t.Fatal(err)
	}
	statement.TotalUsageBytes = 30 + statement.ExtenderPoolWeightBytes - 1
	if err := statement.CheckRows(rows); err == nil {
		t.Fatal("a total without all of Z passed")
	}
	statement = sampleV2()
	if err := statement.CheckSums(31, 2); err == nil {
		t.Fatal("other provider bytes passed")
	}
	if err := statement.CheckSums(30, 3); err == nil {
		t.Fatal("another row count passed")
	}
	if _, err := TotalUsage(math.MaxUint64, 1); err == nil {
		t.Fatal("an overflowing total passed")
	}
	if total, err := TotalUsage(9_000_000_000_000, 1_000_000_000_001); err != nil || total != 10_000_000_000_001 {
		t.Fatal(total, err)
	}
	// A version 1 statement's bytes are its provider rows' alone.
	if err := sample().CheckSums(30, 2); err != nil {
		t.Fatal(err)
	}
}

// An eligible census row unless flags say otherwise.
func censusRow(extender, client byte, unit byte, coldkey byte, flags ledger.CensusFlags) ledger.CensusRow {
	row := ledger.CensusRow{ExtenderID: [16]byte{extender}, ClientID: [16]byte{client}, Flags: flags}
	if flags&ledger.CensusActive != 0 {
		row.Unit, row.Family = [32]byte{unit}, ledger.UnitIPv4
	}
	if coldkey != 0 {
		row.Coldkey = [32]byte{coldkey}
	}
	return row
}

// The worked example's census (WHITEPAPER.md §8.6.5): x1 and x2 in unit u1, x3
// alone in u2, x4 without a wallet; then a revoked and a gated record that never
// count. N = 3, M = 2, and Z' = Z below the cap.
func TestCommittedExtenders(t *testing.T) {
	census := []ledger.CensusRow{
		censusRow(11, 1, 1, 'd', ledger.CensusActive),                    // x1, unit u1, coldkey d
		censusRow(12, 2, 1, 'b', ledger.CensusActive),                    // x2, unit u1, coldkey b
		censusRow(13, 3, 2, 'e', ledger.CensusActive),                    // x3, unit u2, coldkey e
		censusRow(14, 4, 3, 0, ledger.CensusActive),                      // x4, no wallet (E4)
		censusRow(15, 5, 0, 'f', ledger.CensusRevoked),                   // revoked
		censusRow(16, 6, 4, 'g', ledger.CensusActive|ledger.CensusGated), // the operator's own (E5)
	}
	if err := ledger.CheckCensusRows(census); err != nil {
		t.Fatal(err)
	}
	providers := []protocol.LedgerUsageRow{
		{ClientID: [16]byte{20}, Coldkey: [32]byte{'a'}, UsageBytes: 6_000_000_000_000},
		{ClientID: [16]byte{21}, Coldkey: [32]byte{'b'}, UsageBytes: 2_500_000_000_000},
		{ClientID: [16]byte{22}, Coldkey: [32]byte{'c'}, UsageBytes: 500_000_000_000},
		{ClientID: [16]byte{23}, UsageBytes: 1_000_000_000_000},
	}
	const z = 1_000_000_000_001
	split, err := ledger.SplitExtenderPool(z, providers, census)
	if err != nil {
		t.Fatal(err)
	}
	chunks := ledger.EncodeProviderChunks(split.Rows)
	header := &ledger.HeaderExtenders{Bytes: z, Census: []ledger.Ref{ledger.RefOf(ledger.EncodeCensusChunks(census)[0])}, Rows: []ledger.Ref{ledger.RefOf(chunks[0])}}
	committed, err := CommittedExtenders(header, census, split.Rows)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Bytes != z || committed.Eligible != 3 || committed.Units != 2 || committed.Weight != z {
		t.Fatalf("%+v", committed)
	}
	if len(committed.Census) != 1 || committed.Census[0] != header.Census[0].String() || committed.Chunks[0] != header.Rows[0].String() {
		t.Fatalf("%+v", committed)
	}
	pool, err := NewExtenderPool(600_000_000_001, 400_000_000_000, 12, 3, committed)
	if err != nil {
		t.Fatal(err)
	}
	statement := sample()
	statement.ExtenderPool = pool
	statement.TotalUsageBytes = 30 + z
	if err := statement.CheckExtenders(committed); err != nil {
		t.Fatal(err)
	}
	if _, err := NewExtenderPool(600_000_000_001, 400_000_000_001, 12, 3, committed); err == nil {
		t.Fatal("parts that do not sum to Z made an extender pool")
	}
	// Each committed fact must be carried exactly.
	for name, change := range map[string]func(*Extenders){
		"bytes":    func(c *Extenders) { c.Bytes++ },
		"census":   func(c *Extenders) { c.Census = []string{ref(9)} },
		"chunks":   func(c *Extenders) { c.Chunks = []string{} },
		"eligible": func(c *Extenders) { c.Eligible = 2 },
		"units":    func(c *Extenders) { c.Units = 1 },
		"weight":   func(c *Extenders) { c.Weight-- },
	} {
		other := *committed
		change(&other)
		if err := statement.CheckExtenders(&other); err == nil {
			t.Fatal("a statement passed against another component:", name)
		}
	}
	// The version follows the header: v1 without a component, v2 with one.
	if err := statement.CheckExtenders(nil); err == nil {
		t.Fatal("a version 2 statement passed for an epoch without a component")
	}
	if err := sample().CheckExtenders(committed); err == nil {
		t.Fatal("a version 1 statement passed for an epoch with a component")
	}
	if err := sample().CheckExtenders(nil); err != nil {
		t.Fatal(err)
	}
}

// With no eligible extender Z is dropped: the weight is 0 and there are no share
// rows, but Z still counts in total_usage_bytes; and a census with no rows is [].
func TestCommittedExtendersEmpty(t *testing.T) {
	census := []ledger.CensusRow{censusRow(11, 1, 1, 0, ledger.CensusActive)}
	const z = 5_000
	header := &ledger.HeaderExtenders{Bytes: z, Census: []ledger.Ref{ledger.RefOf(ledger.EncodeCensusChunks(census)[0])}, Rows: []ledger.Ref{}}
	committed, err := CommittedExtenders(header, census, nil)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Eligible != 0 || committed.Units != 0 || committed.Weight != 0 || committed.Chunks == nil || len(committed.Chunks) != 0 {
		t.Fatalf("%+v", committed)
	}
	pool, err := NewExtenderPool(0, z, 1, 1, committed)
	if err != nil {
		t.Fatal(err)
	}
	seal := goldenSeal(t)
	statement := sample()
	statement.ExtenderPool, statement.TotalUsageBytes = pool, 30+z
	raw := seal(statement)
	if !strings.Contains(string(raw), `"extender_pool_bytes":5000,"extender_pool_weight_bytes":0,`) || !strings.Contains(string(raw), `"extender_chunks":[]`) {
		t.Fatalf("%s", raw)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := back.CheckExtenders(committed); err != nil {
		t.Fatal(err)
	}
	if err := back.CheckSums(30, 2); err != nil {
		t.Fatal(err)
	}

	empty := &ledger.HeaderExtenders{Census: []ledger.Ref{}, Rows: []ledger.Ref{}}
	none, err := CommittedExtenders(empty, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pool, err = NewExtenderPool(0, 0, 0, 0, none)
	if err != nil {
		t.Fatal(err)
	}
	statement = sample()
	statement.ExtenderPool = pool
	if raw := seal(statement); !strings.Contains(string(raw), `"extender_census":[],"extender_units":0,"eligible_extenders":0,"extender_chunks":[]`) {
		t.Fatalf("%s", raw)
	}
	if _, err := CommittedExtenders(nil, nil, nil); err == nil {
		t.Fatal("no component read as one")
	}
}
