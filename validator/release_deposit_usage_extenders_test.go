//go:build linux || darwin

// The deposit audit of a source epoch whose committed header carries an extender
// component (WHITEPAPER.md §8.6.4): its statement is version 2, read under its own
// evidence kind, and must carry exactly what the header, its census chunks and its
// share-row chunks hold; its total_usage_bytes, which the deposit prices, is the
// provider rows' usage plus all of Z. Every run checks the pure measurement
// verifier and an offline replay, as the version 1 tests do.
package validator

import (
	"context"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/urnetwork/connect"

	"github.com/urfoundation/sn/demand/pricesheet"
	"github.com/urfoundation/sn/demand/usage"
	"github.com/urfoundation/sn/ledger"
	"github.com/urfoundation/sn/payoutartifact"
	"github.com/urfoundation/sn/protocol"
)

// The parts of Z the operator attests: intra-platform and intra-network bytes, and
// the contracts and payer networks behind them.
type depositExtenderTestParts struct {
	intraPlatform, intraNetwork, contracts, payers uint64
}

// A depositUsageTestFixture whose source epoch 0 has an extender component:
// Z bytes, a census and the share rows of its split.
type depositExtenderTestFixture struct {
	*depositUsageTestFixture
	z         uint64
	parts     depositExtenderTestParts
	census    []ledger.CensusRow
	shares    []protocol.LedgerUsageRow
	censusRef []ledger.Ref
	shareRef  []ledger.Ref
}

// The worked example's census (WHITEPAPER.md §8.6.5) in miniature: x1 and x2
// share unit u1, x3 is alone in u2, x4 has no wallet (E4) and x5 is revoked. So
// N = 3 and M = 2.
func depositExtenderTestCensus() []ledger.CensusRow {
	active := func(extender, client, unit, coldkey byte) ledger.CensusRow {
		row := ledger.CensusRow{ExtenderID: [16]byte{extender}, ClientID: [16]byte{client}, Unit: [32]byte{unit}, Family: ledger.UnitIPv4, Flags: ledger.CensusActive}
		if coldkey != 0 {
			row.Coldkey = [32]byte{coldkey}
		}
		return row
	}
	return []ledger.CensusRow{
		active(0x21, 0x11, 0x31, 0xd1),
		active(0x22, 0x12, 0x31, 0xd2),
		active(0x23, 0x13, 0x32, 0xd3),
		active(0x24, 0x14, 0x33, 0),
		{ExtenderID: [16]byte{0x25}, ClientID: [16]byte{0x15}, Coldkey: [32]byte{0xd5}, Flags: ledger.CensusRevoked},
	}
}

// One GiB and a byte: below W_k, the 2 GiB of eligible provider usage in
// depositUsageTestRows (its other row is head excluded), so Z' = Z.
const depositExtenderTestZ = uint64(1<<30 + 1)

// A fixture whose source epoch 0 carries the worked example's Z.
func newDepositExtenderTestFixture(t *testing.T) *depositExtenderTestFixture {
	t.Helper()
	return &depositExtenderTestFixture{depositUsageTestFixture: newDepositUsageTestFixture(t), z: depositExtenderTestZ,
		parts: depositExtenderTestParts{intraPlatform: 1, intraNetwork: depositExtenderTestZ - 1, contracts: 5, payers: 2}}
}

// Publishes rows, census and the share rows of z's split as source
// epoch 0's chunks, and a v3 header listing them.
func (self *depositExtenderTestFixture) publishExtenderLedger(t *testing.T, rows []protocol.LedgerUsageRow, z uint64, census []ledger.CensusRow) {
	t.Helper()
	split, err := ledger.SplitExtenderPool(z, rows, census)
	if err != nil {
		t.Fatal(err)
	}
	self.publishComponent(t, rows, z, census, split.Rows)
}

// Publishes a component with the share rows given, split or not.
func (self *depositExtenderTestFixture) publishComponent(t *testing.T, rows []protocol.LedgerUsageRow, z uint64, census []ledger.CensusRow, shares []protocol.LedgerUsageRow) {
	t.Helper()
	encoded := ledger.EncodeProviderChunks(rows)
	self.rows, self.chunks = rows, make([]ledger.Ref, len(encoded))
	for index, chunk := range encoded {
		self.chunks[index] = self.operator.putObject(chunk)
	}
	self.z, self.census, self.shares = z, census, shares
	self.censusRef, self.shareRef = []ledger.Ref{}, []ledger.Ref{}
	for _, chunk := range ledger.EncodeCensusChunks(census) {
		self.censusRef = append(self.censusRef, self.operator.putObject(chunk))
	}
	for _, chunk := range ledger.EncodeProviderChunks(shares) {
		self.shareRef = append(self.shareRef, self.operator.putObject(chunk))
	}
	self.publishExtenderHeader(t)
}

// Publishes source epoch 0's v3 header, listing its provider, census and
// share-row chunks.
func (self *depositExtenderTestFixture) publishExtenderHeader(t *testing.T) {
	t.Helper()
	header, err := ledger.EncodeHeader(&ledger.Header{NoID: 1, Epoch: 0, Epochs: []ledger.HeaderEpoch{{Epoch: 0, Providers: self.chunks,
		Extenders: &ledger.HeaderExtenders{Bytes: self.z, Census: self.censusRef, Rows: self.shareRef}}}, Accounts: []ledger.Ref{}})
	if err != nil {
		t.Fatal(err)
	}
	self.header = self.operator.putObject(header)
}

// What the published component holds, as its statement must carry it.
func (self *depositExtenderTestFixture) committed(t *testing.T) *usage.Extenders {
	t.Helper()
	committed, err := usage.CommittedExtenders(&ledger.HeaderExtenders{Bytes: self.z, Census: self.censusRef, Rows: self.shareRef}, self.census, self.shares)
	if err != nil {
		t.Fatal(err)
	}
	return committed
}

// The operator's version 2 statement of the published component: the provider
// rows' usage plus all of Z, and the extender pool. mutate runs before it is
// sealed.
func (self *depositExtenderTestFixture) statementV2(t *testing.T, mutate func(*usage.Statement)) *usage.Statement {
	t.Helper()
	pool, err := usage.NewExtenderPool(self.parts.intraPlatform, self.parts.intraNetwork, self.parts.contracts, self.parts.payers, self.committed(t))
	if err != nil {
		t.Fatal(err)
	}
	return self.statement(t, func(statement *usage.Statement) {
		statement.ExtenderPool = pool
		statement.TotalUsageBytes += self.z
		if mutate != nil {
			mutate(statement)
		}
	})
}

// Everything a compliant operator does for source epoch 0 but its
// deposit.
func (self *depositExtenderTestFixture) settleV2(t *testing.T) {
	t.Helper()
	self.publishExtenderLedger(t, depositUsageTestRows(), self.z, depositExtenderTestCensus())
	self.commitHeader(t)
	self.operator.publish(t, self.statementV2(t, nil), "")
}

// What the provider rows' usage and all of Z owe at a conviction.
func (self *depositExtenderTestFixture) owedV2(t *testing.T, entry *pricesheet.Entry, conviction *big.Int) *big.Int {
	t.Helper()
	total, _, err := usage.SumRows(self.rows)
	if err != nil {
		t.Fatal(err)
	}
	required, _, err := protocol.RequiredDepositRao(total+self.z, depositUsageTestUsers, conviction, entry.DepositPolicy(self.cfg.Policy.Deposit))
	if err != nil {
		t.Fatal(err)
	}
	return required
}

// Requires the run's audit refused with status and message under evidence kind,
// with no weight.
func requireDepositExtenderRefused(t *testing.T, run depositUsageTestRun, status, message, kind string) {
	t.Helper()
	audit := run.audit
	if audit.Status != status || audit.Compliant || audit.Disposition != "zero_pool_weight" || run.score != nil || audit.UsageEvidence != kind || !strings.Contains(audit.Error, message) {
		t.Fatalf("want %s with %q under %s and no weight, got score %v and %+v", status, message, kind, run.score, audit)
	}
}

// The compliant audit of an epoch with an extender component: the shared formula
// prices the provider rows' 3 GiB plus all of Z, the statement is read under
// epoch-usage-v2 only, and replay reproduces it without the operator's API.
func TestDepositUsageExtendersCompliantAuditPricesAllOfZ(t *testing.T) {
	fixture := newDepositExtenderTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settleV2(t)
	committed := fixture.committed(t)
	if committed.Eligible != 3 || committed.Units != 2 || committed.Weight != depositExtenderTestZ || len(committed.Census) != 1 || len(committed.Chunks) != 1 {
		t.Fatalf("the worked census: %+v", committed)
	}
	// x1 and x2 share u1's half, the lower client id taking the odd byte; x3 has u2's.
	if want := []uint64{1<<28 + 1, 1 << 28, 1 << 29}; len(fixture.shares) != 3 || fixture.shares[0].UsageBytes != want[0] || fixture.shares[1].UsageBytes != want[1] || fixture.shares[2].UsageBytes != want[2] {
		t.Fatalf("the split: %+v", fixture.shares)
	}
	required := fixture.owedV2(t, entry, big.NewInt(0))
	if fixture.owed(t, entry, big.NewInt(0)).Cmp(required) >= 0 {
		t.Fatal("Z added nothing to the deposit")
	}
	fixture.fund(t, required, big.NewInt(0))
	run := fixture.run(t)
	audit := run.audit
	statementBytes, err := fixture.statementV2(t, nil).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != DepositAuditCompliant || !audit.Compliant || audit.Error != "" || audit.UsageBytes != 3<<30+depositExtenderTestZ || audit.Users != depositUsageTestUsers ||
		audit.RequiredDepositRao != required.String() || audit.UsageEvidence != usage.EvidenceKindV2 || audit.HttpObservationHash == "" ||
		audit.ArtifactHash != ReleaseMeasurementContentHash(statementBytes) || audit.CommittedArtifactHash != releaseHex32(fixture.header) {
		t.Fatalf("compliant audit %+v", audit)
	}
	if run.score == nil || run.score.Sign() <= 0 {
		t.Fatalf("no weight: %v", run.score)
	}
	fixture.operator.lock.Lock()
	kinds := slices.Clone(fixture.operator.historyKinds)
	fixture.operator.lock.Unlock()
	if len(kinds) == 0 || slices.ContainsFunc(kinds, func(kind string) bool { return kind != usage.EvidenceKindV2 }) {
		t.Fatalf("the audit read statement kinds %v", kinds)
	}
	// A deposit sized from the provider rows alone is short.
	fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
	fixture.advance(330)
	requireDepositExtenderRefused(t, fixture.run(t), DepositAuditMismatch, "does not equal", usage.EvidenceKindV2)
}

// The cap and the empty set. Above W_k, Z' is W_k and the share rows sum to it, but
// the deposit still prices all of Z. With no eligible extender Z is dropped from
// the allocation (Z' = 0, no share rows), and still priced.
func TestDepositUsageExtendersCapAndEmptySet(t *testing.T) {
	gated := depositExtenderTestCensus()
	for index := range gated {
		if gated[index].Flags&ledger.CensusActive != 0 {
			gated[index].Flags |= ledger.CensusGated
		}
	}
	for name, test := range map[string]struct {
		z           uint64
		census      []ledger.CensusRow
		weight      uint64
		chunks      int
		eligible, m uint64
	}{
		"above the cap": {z: 3 << 30, census: depositExtenderTestCensus(), weight: 2 << 30, chunks: 1, eligible: 3, m: 2},
		"at the cap":    {z: 2 << 30, census: depositExtenderTestCensus(), weight: 2 << 30, chunks: 1, eligible: 3, m: 2},
		"none eligible": {z: depositExtenderTestZ, census: gated, weight: 0, chunks: 0},
		"no census":     {z: depositExtenderTestZ, census: []ledger.CensusRow{}, weight: 0, chunks: 0},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositExtenderTestFixture(t)
			fixture.parts = depositExtenderTestParts{intraPlatform: 0, intraNetwork: test.z, contracts: 1, payers: 1}
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.publishExtenderLedger(t, depositUsageTestRows(), test.z, test.census)
			fixture.commitHeader(t)
			committed := fixture.committed(t)
			if committed.Weight != test.weight || len(committed.Chunks) != test.chunks || committed.Eligible != test.eligible || committed.Units != test.m {
				t.Fatalf("component %+v", committed)
			}
			fixture.operator.publish(t, fixture.statementV2(t, nil), "")
			fixture.fund(t, fixture.owedV2(t, entry, big.NewInt(0)), big.NewInt(0))
			run := fixture.run(t)
			if !run.audit.Compliant || run.audit.UsageBytes != 3<<30+test.z || run.audit.UsageEvidence != usage.EvidenceKindV2 {
				t.Fatalf("audit %+v", run.audit)
			}
		})
	}
}

// Each fact a version 2 statement carries must be the committed component's, and
// its bytes the provider rows' plus all of Z: a statement that differs in any is
// artifact_invalid, retained with the statement it refused.
func TestDepositUsageExtendersStatementMismatchesAreInvalid(t *testing.T) {
	for name, test := range map[string]struct {
		mutate  func(*usage.Statement)
		message string
	}{
		"Z": {mutate: func(s *usage.Statement) {
			s.IntraNetworkBytes++
			s.ExtenderPoolBytes++
			s.TotalUsageBytes++
		}, message: "is not the header's"},
		"N":                 {mutate: func(s *usage.Statement) { s.EligibleExtenders = 4 }, message: "the census holds 3 in 2"},
		"M":                 {mutate: func(s *usage.Statement) { s.ExtenderUnits = 1 }, message: "the census holds 3 in 2"},
		"Z'":                {mutate: func(s *usage.Statement) { s.ExtenderPoolWeightBytes-- }, message: "is not the share rows'"},
		"a total without Z": {mutate: func(s *usage.Statement) { s.TotalUsageBytes -= depositExtenderTestZ }, message: "extender pool bytes"},
		"a total with Z'":   {mutate: func(s *usage.Statement) { s.TotalUsageBytes-- }, message: "extender pool bytes"},
		"a row count":       {mutate: func(s *usage.Statement) { s.ProviderRows++ }, message: "extender pool bytes"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositExtenderTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.publishExtenderLedger(t, depositUsageTestRows(), fixture.z, depositExtenderTestCensus())
			fixture.commitHeader(t)
			fixture.operator.publish(t, fixture.statementV2(t, test.mutate), "")
			fixture.fund(t, fixture.owedV2(t, entry, big.NewInt(0)), big.NewInt(0))
			run := fixture.run(t)
			requireDepositExtenderRefused(t, run, DepositAuditInvalid, test.message, usage.EvidenceKindV2)
			if run.audit.ArtifactHash == "" {
				t.Fatal("the invalid audit lost the statement it refused")
			}
		})
	}
}

// A statement that names other census or share-row chunks is another build's, as
// one that names other provider chunks is: nothing names the committed chunks yet.
func TestDepositUsageExtendersOtherChunksArePendingThenUnavailable(t *testing.T) {
	for name, mutate := range map[string]func(*usage.Statement){
		"census":     func(s *usage.Statement) { s.ExtenderCensus = []string{ledger.RefOf([]byte("another census")).String()} },
		"share rows": func(s *usage.Statement) { s.ExtenderChunks = []string{ledger.RefOf([]byte("another split")).String()} },
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositExtenderTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.publishExtenderLedger(t, depositUsageTestRows(), fixture.z, depositExtenderTestCensus())
			fixture.commitHeader(t)
			fixture.operator.publish(t, fixture.statementV2(t, mutate), "")
			fixture.fund(t, fixture.owedV2(t, entry, big.NewInt(0)), big.NewInt(0))
			requireDepositExtenderRefused(t, fixture.run(t), DepositAuditUnavailablePending, "no usage statement names", usage.EvidenceKindV2)
			fixture.advance(365)
			requireDepositExtenderRefused(t, fixture.run(t), DepositAuditUnavailable, "no usage statement names", usage.EvidenceKindV2)
		})
	}
}

// The committed share rows must sum to the capped pool, Z' = min(Z, W_k), or to 0
// with no eligible extender. Rows of the uncapped Z, or rows with none eligible,
// are the operator's invalid ledger, whatever its statement says.
func TestDepositUsageExtendersCommittedShareRowsMustBeCapped(t *testing.T) {
	census := depositExtenderTestCensus()
	rows := depositUsageTestRows()
	uncapped := func() []protocol.LedgerUsageRow {
		// The split of Z = 3 GiB as if W_k were larger: rows summing to all of Z.
		wide := []protocol.LedgerUsageRow{{ClientID: [16]byte{1}, Coldkey: [32]byte{1}, UsageBytes: 4 << 30}}
		split, err := ledger.SplitExtenderPool(3<<30, wide, census)
		if err != nil {
			t.Fatal(err)
		}
		return split.Rows
	}()
	gated := slices.Clone(census)
	for index := range gated {
		if gated[index].Flags&ledger.CensusActive != 0 {
			gated[index].Flags |= ledger.CensusGated
		}
	}
	for name, test := range map[string]struct {
		z      uint64
		census []ledger.CensusRow
		shares []protocol.LedgerUsageRow
	}{
		"uncapped":      {z: 3 << 30, census: census, shares: uncapped},
		"none eligible": {z: depositExtenderTestZ, census: gated, shares: []protocol.LedgerUsageRow{{ClientID: [16]byte{0x11}, Coldkey: [32]byte{0xd1}, UsageBytes: 7}}},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositExtenderTestFixture(t)
			fixture.parts = depositExtenderTestParts{intraNetwork: test.z, contracts: 1, payers: 1}
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.publishComponent(t, rows, test.z, test.census, test.shares)
			fixture.commitHeader(t)
			// A statement that carries the committed facts exactly does not help.
			committed := fixture.committed(t)
			pool := &usage.ExtenderPool{IntraNetworkBytes: test.z, ExtenderPoolBytes: test.z, ExtenderPoolWeightBytes: committed.Weight, ExtenderPoolContracts: 1, ExtenderPoolPayers: 1,
				ExtenderCensus: committed.Census, ExtenderUnits: max(committed.Units, 1), EligibleExtenders: max(committed.Eligible, 1), ExtenderChunks: committed.Chunks}
			fixture.operator.publish(t, fixture.statement(t, func(s *usage.Statement) {
				s.ExtenderPool, s.TotalUsageBytes = pool, s.TotalUsageBytes+test.z
			}), "")
			fixture.fund(t, fixture.owedV2(t, entry, big.NewInt(0)), big.NewInt(0))
			run := fixture.run(t)
			requireDepositExtenderRefused(t, run, DepositAuditInvalid, "the capped extender pool", usage.EvidenceKindV2)
			if fixture.operator.history() != 0 {
				t.Fatal("an invalid component reached the statement history")
			}
		})
	}
}

// A component that breaks the ledger's rules is the operator's invalid ledger, read
// one chunk at a time like provider rows.
func TestDepositUsageExtendersMalformedComponentIsInvalid(t *testing.T) {
	census := depositExtenderTestCensus()
	rows := depositUsageTestRows()
	split, err := ledger.SplitExtenderPool(depositExtenderTestZ, rows, census)
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		census  []byte
		shares  []byte
		message string
	}{
		"census unsorted":     {census: ledger.EncodeCensusChunks([]ledger.CensusRow{census[1], census[0], census[2], census[3], census[4]})[0], message: ledger.ErrCensusUnsorted.Error()},
		"census partial row":  {census: ledger.EncodeCensusChunks(census)[0][:ledger.CensusRowBytes+1], message: ledger.ErrChunkLength.Error()},
		"census flags":        {census: append(slices.Clone(ledger.EncodeCensusChunks(census)[0][:ledger.CensusRowBytes-1]), 0x80), message: ledger.ErrCensusEncoding.Error()},
		"share rows unsorted": {shares: ledger.EncodeProviderChunks([]protocol.LedgerUsageRow{split.Rows[1], split.Rows[0], split.Rows[2]})[0], message: ledger.ErrRowsUnsorted.Error()},
		"share row encoding":  {shares: append(slices.Clone(ledger.EncodeProviderChunks(split.Rows)[0][:ledger.ProviderRowBytes-1]), 2), message: ledger.ErrRowEncoding.Error()},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositExtenderTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.publishExtenderLedger(t, rows, fixture.z, census)
			if test.census != nil {
				fixture.censusRef = []ledger.Ref{fixture.operator.putObject(test.census)}
			}
			if test.shares != nil {
				fixture.shareRef = []ledger.Ref{fixture.operator.putObject(test.shares)}
			}
			fixture.publishExtenderHeader(t)
			fixture.commitHeader(t)
			fixture.fund(t, fixture.owedV2(t, entry, big.NewInt(0)), big.NewInt(0))
			requireDepositExtenderRefused(t, fixture.run(t), DepositAuditInvalid, test.message, usage.EvidenceKindV2)
			if fixture.operator.history() != 0 {
				t.Fatal("an invalid component reached the statement history")
			}
		})
	}
}

// The statement's version follows the committed header. A covered epoch reads only
// epoch-usage-v2, so a version 1 statement is not its statement; an epoch without a
// component reads only epoch-usage-v1, exactly as before the amendment. A statement
// published under the other version's kind is the operator's invalid evidence.
func TestDepositUsageExtendersStatementVersionFollowsTheHeader(t *testing.T) {
	t.Run("a v1 statement for a covered epoch", func(t *testing.T) {
		fixture := newDepositExtenderTestFixture(t)
		entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
		fixture.price(entry)
		fixture.publishExtenderLedger(t, depositUsageTestRows(), fixture.z, depositExtenderTestCensus())
		fixture.commitHeader(t)
		fixture.operator.publish(t, fixture.statement(t, nil), "")
		fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
		requireDepositExtenderRefused(t, fixture.run(t), DepositAuditUnavailablePending, "no usage statement is published", usage.EvidenceKindV2)
	})
	t.Run("a v1 statement under the v2 kind", func(t *testing.T) {
		fixture := newDepositExtenderTestFixture(t)
		entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
		fixture.price(entry)
		fixture.publishExtenderLedger(t, depositUsageTestRows(), fixture.z, depositExtenderTestCensus())
		fixture.commitHeader(t)
		fixture.operator.publishAs(t, fixture.statement(t, nil), "", usage.EvidenceKindV2)
		fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
		requireDepositExtenderRefused(t, fixture.run(t), DepositAuditInvalid, "is published as", usage.EvidenceKindV2)
	})
	// An epoch without a component, with a version 2 statement as if it had one.
	uncovered := func(t *testing.T) (*depositExtenderTestFixture, *pricesheet.Entry, *usage.Statement) {
		fixture := newDepositExtenderTestFixture(t)
		entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
		fixture.price(entry)
		fixture.publishLedger(t, depositUsageTestRows())
		fixture.commitHeader(t)
		fixture.census, fixture.censusRef, fixture.shareRef = depositExtenderTestCensus(), []ledger.Ref{ledger.RefOf([]byte("census"))}, []ledger.Ref{ledger.RefOf([]byte("rows"))}
		split, err := ledger.SplitExtenderPool(fixture.z, fixture.rows, fixture.census)
		if err != nil {
			t.Fatal(err)
		}
		fixture.shares = split.Rows
		return fixture, entry, fixture.statementV2(t, nil)
	}
	t.Run("a v2 statement for an uncovered epoch", func(t *testing.T) {
		fixture, entry, statement := uncovered(t)
		fixture.operator.publish(t, statement, "")
		fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
		requireDepositExtenderRefused(t, fixture.run(t), DepositAuditUnavailablePending, "no usage statement is published", usage.EvidenceKind)
	})
	t.Run("a v2 statement under the v1 kind", func(t *testing.T) {
		fixture, entry, statement := uncovered(t)
		fixture.operator.publishAs(t, statement, "", usage.EvidenceKind)
		fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
		requireDepositExtenderRefused(t, fixture.run(t), DepositAuditInvalid, "is published as", usage.EvidenceKind)
	})
	t.Run("the v1 audit of an uncovered epoch is unchanged", func(t *testing.T) {
		fixture, entry, statement := uncovered(t)
		fixture.operator.publish(t, statement, "")
		fixture.operator.publish(t, fixture.statement(t, nil), "")
		fixture.fund(t, fixture.owed(t, entry, big.NewInt(0)), big.NewInt(0))
		run := fixture.run(t)
		if !run.audit.Compliant || run.audit.UsageEvidence != usage.EvidenceKind || run.audit.UsageBytes != 3<<30 {
			t.Fatalf("audit %+v", run.audit)
		}
	})
}

// Two verified statements of one committed component that disagree in any total or
// extender field are equivocation, whichever came first; equal ones in two
// envelopes are one.
func TestDepositUsageExtendersEquivocation(t *testing.T) {
	for name, test := range map[string]struct {
		mutate  func(*usage.Statement)
		message string
	}{
		"contracts": {mutate: func(s *usage.Statement) { s.ExtenderPoolContracts++ }, message: "disagree on the extender pool"},
		"payers":    {mutate: func(s *usage.Statement) { s.ExtenderPoolPayers-- }, message: "disagree on the extender pool"},
		"parts": {mutate: func(s *usage.Statement) {
			s.IntraPlatformBytes++
			s.IntraNetworkBytes--
		}, message: "disagree on the extender pool"},
		"users": {mutate: func(s *usage.Statement) { s.TotalUsers++ }, message: "disagree:"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDepositExtenderTestFixture(t)
			entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
			fixture.price(entry)
			fixture.settleV2(t)
			fixture.operator.publish(t, fixture.statementV2(t, test.mutate), "")
			fixture.fund(t, fixture.owedV2(t, entry, big.NewInt(0)), big.NewInt(0))
			requireDepositExtenderRefused(t, fixture.run(t), DepositAuditEquivocation, test.message, usage.EvidenceKindV2)
		})
	}
	t.Run("equal statements", func(t *testing.T) {
		fixture := newDepositExtenderTestFixture(t)
		entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
		fixture.price(entry)
		fixture.settleV2(t)
		fixture.operator.publish(t, fixture.statementV2(t, nil), "the same statement again")
		fixture.fund(t, fixture.owedV2(t, entry, big.NewInt(0)), big.NewInt(0))
		if run := fixture.run(t); !run.audit.Compliant {
			t.Fatalf("audit %+v", run.audit)
		}
	})
}

// The census and share-row chunks count against the root's byte budget like
// provider chunks: a budget that holds the header and provider rows but not the
// census leaves the pool pending, retained so replay agrees, and no statement is
// read.
func TestDepositUsageExtendersCountAgainstTheRootsBudget(t *testing.T) {
	fixture := newDepositExtenderTestFixture(t)
	fixture.price(depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000))
	fixture.settleV2(t)
	fixture.fund(t, big.NewInt(0), big.NewInt(0))
	fixture.operator.lock.Lock()
	ledgerBytes := uint64(len(fixture.operator.objects[fixture.header.String()]))
	for _, ref := range fixture.chunks {
		ledgerBytes += uint64(len(fixture.operator.objects[ref.String()]))
	}
	fixture.operator.lock.Unlock()
	ctx := WithSettlementV2RuntimeOptions(t.Context(), SettlementV2RuntimeOptions{LedgerMaxBytes: ledgerBytes + ledger.CensusRowBytes - 1})
	decision := fixture.decision()
	_, pools, audits, err := fixture.steerer.gatherPoolWeightsV2(ctx, fixture.snapshot(), map[uint64]map[connect.Id]bool{}, fixture.hotkeyUIDs(), decision)
	if err != nil {
		t.Fatal(err)
	}
	decision.Pools, decision.DepositAudits = pools, audits
	requireDepositExtenderRefused(t, depositUsageTestRun{audit: audits[0]}, DepositAuditUnavailablePending, depositUsageByteBudgetSpent, usage.EvidenceKindV2)
	if replayed := fixture.replay(t, decision); !slices.Equal(replayed, audits) {
		t.Fatalf("replay %+v differs from live %+v", replayed, audits)
	}
	if fixture.operator.history() != 0 {
		t.Fatal("an unread component reached the statement history")
	}
	// With room for the whole component the next pass completes it.
	ctx = WithSettlementV2RuntimeOptions(t.Context(), SettlementV2RuntimeOptions{LedgerMaxBytes: DefaultLedgerMaxBytes})
	fixture.advance(330)
	decision = fixture.decision()
	if _, _, audits, err = fixture.steerer.gatherPoolWeightsV2(ctx, fixture.snapshot(), map[uint64]map[connect.Id]bool{}, fixture.hotkeyUIDs(), decision); err != nil {
		t.Fatal(err)
	}
	if audits[0].Status != DepositAuditMismatch || audits[0].UsageBytes != 3<<30+depositExtenderTestZ {
		t.Fatalf("the full read: %+v", audits[0])
	}
}

// Live steering verifies a component once per process and judges from the memo
// after, exactly as from a fresh read.
func TestDepositUsageExtendersAreReadOncePerEpoch(t *testing.T) {
	fixture := newDepositExtenderTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settleV2(t)
	required := fixture.owedV2(t, entry, big.NewInt(0))
	fixture.fund(t, required, big.NewInt(0))
	memo := fixture.steerer.depositUsageMemo
	fills := func() uint64 {
		memo.lock.Lock()
		defer memo.lock.Unlock()
		return memo.fills
	}
	first := fixture.run(t)
	if !first.audit.Compliant || fills() != 1 {
		t.Fatalf("first pass: %d reads, %+v", fills(), first.audit)
	}
	// The memo's copy is its own: changing what a reader got changes nothing kept.
	got, ok := memo.get(depositUsageMemoKey{noID: 1, epoch: 0, ledgerHash: fixture.header})
	if !ok || got.extenders == nil || got.extenders.Units != 2 {
		t.Fatalf("memo %+v", got)
	}
	got.extenders.Census[0], got.extenders.Units = "changed", 9
	fixture.advance(330)
	second := fixture.run(t)
	if !second.audit.Compliant || fills() != 1 || second.audit.UsageBytes != first.audit.UsageBytes || second.audit.RequiredDepositRao != first.audit.RequiredDepositRao {
		t.Fatalf("a later pass: %d reads, %+v", fills(), second.audit)
	}
}

// Retention keeps every object replay of a covered epoch can reach, its census and
// share-row chunks included, and an archive replays it without validator state.
func TestDepositUsageExtendersRetentionAndArchiveReplay(t *testing.T) {
	fixture := newDepositExtenderTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.settleV2(t)
	fixture.fund(t, fixture.owedV2(t, entry, big.NewInt(0)), big.NewInt(0))
	run := fixture.run(t)
	if !run.audit.Compliant {
		t.Fatalf("%+v", run.audit)
	}
	store := depositUsageObjectsAt(fixture.cfg.StateDir)
	all := append(append(append([]ledger.Ref{fixture.header}, fixture.chunks...), fixture.censusRef...), fixture.shareRef...)
	if len(fixture.censusRef) != 1 || len(fixture.shareRef) != 1 {
		t.Fatalf("component chunks %v %v", fixture.censusRef, fixture.shareRef)
	}
	held := func(ref ledger.Ref) bool {
		_, found, err := store.own.Object(t.Context(), ref)
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	if err := pruneDepositUsageObjectsV2(t.Context(), fixture.cfg.StateDir, 1); err != nil {
		t.Fatal(err)
	}
	for _, ref := range all {
		if !held(ref) {
			t.Fatalf("the retained decision's object %s was pruned", ref)
		}
	}
	if replayed := fixture.replay(t, run.decision); !slices.Equal(replayed, run.decision.DepositAudits) {
		t.Fatalf("replay after retention: %+v", replayed)
	}

	reader, err := NewHTTPArtifactReader(fixture.cfg.Operators[0].APIURL, fixture.cfg.DeploymentID, fixture.cfg.Netuid)
	if err != nil {
		t.Fatal(err)
	}
	_, path, err := releaseDepositObservationRequestV2(&fixture.cfg, fixture.steerer.hotkey.PublicKey(), run.decision, run.audit, reader)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sources := map[ReleaseEvidenceV2CaptureSource][]byte{}
	if err := emitDepositUsageObjectsV2(t.Context(), fixture.cfg.StateDir, fixture.cfg.Operators[0].APIURL, run.audit, depositUsageTestExchanges(t, observation), func(source ReleaseEvidenceV2CaptureSource, encoded []byte) error {
		sources[source] = encoded
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, ref := range all {
		data, found := sources[ReleaseEvidenceV2CaptureSource{Kind: releaseUsageLedgerObjectCaptureKind, Name: ref.String()}]
		if !found || ledger.RefOf(data) != ref {
			t.Fatalf("the capture lacks object %s", ref)
		}
	}
	if len(sources) != len(all) {
		t.Fatalf("the capture holds %d objects, not %d", len(sources), len(all))
	}
	name, err := filepath.Rel(fixture.cfg.StateDir, path)
	if err != nil {
		t.Fatal(err)
	}
	sources[ReleaseEvidenceV2CaptureSource{Kind: "private", Name: filepath.ToSlash(name)}] = observation
	archiveSources := map[ReleaseEvidenceV2CaptureSource]ReleaseEvidenceV2ArchiveSource{}
	for source, encoded := range sources {
		archiveSources[source] = ReleaseEvidenceV2ArchiveSource{Source: source, SizeBytes: uint64(len(encoded)), ContentHash: ReleaseMeasurementContentHash(encoded)}
	}
	observed, err := fixture.observe(t, run.decision)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(fixture.cfg.StateDir); err != nil {
		t.Fatal(err)
	}
	root := newAttemptSettlementRuntimeV2TestStateDir(t)
	owner := &releaseEvidenceV2ArchiveOwner{ctx: t.Context(), cfg: fixture.cfg, root: root, sources: archiveSources, readSource: func(_ context.Context, source ReleaseEvidenceV2CaptureSource) ([]byte, error) {
		return sources[source], nil
	}}
	if owner.anchor, err = os.Lstat(root); err != nil {
		t.Fatal(err)
	}
	custody := &releaseEvidenceV2StartupReferences{remaining: fixture.cfg.EvidenceV2.Bounds.MaxHistoryBytes, archive: owner}
	replayed, err := fixture.history().historicalDepositAuditWithCaptureV2(t.Context(), observed, observed.operators[0], run.audit, run.decision, custody)
	if err != nil || replayed != run.audit {
		t.Fatalf("archive replay %+v differs from %+v: %v", replayed, run.audit, err)
	}
}

// The pure judgment: with a committed component the statement must be version 2
// and carry it, and its bytes are the rows' plus all of Z; without one a version 2
// statement is refused, and a version 1 one is judged exactly as before.
func TestEvaluateDepositUsageExtenders(t *testing.T) {
	fixture := newDepositExtenderTestFixture(t)
	entry := depositPriceTestEntry(t, fixture.cfg.Netuid, 1, 1_000_000_000)
	fixture.price(entry)
	fixture.publishExtenderLedger(t, depositUsageTestRows(), fixture.z, depositExtenderTestCensus())
	committed := fixture.committed(t)
	policy := entry.DepositPolicy(fixture.cfg.Policy.Deposit)
	chunks := []string{fixture.chunks[0].String()}
	expectation := &DepositUsageExpectation{DeploymentID: fixture.cfg.DeploymentID, ChainID: fixture.cfg.ChainID, GenesisHash: fixture.cfg.GenesisHash, Netuid: fixture.cfg.Netuid,
		Coordinator: fixture.statement(t, nil).Coordinator, VaultV2: fixture.addresses.Vault, PolicyHash: fixture.cfg.PolicyHash, Epoch: 0, NoID: 1, Signer: fixture.statement(t, nil).Signer,
		Start: fixturePayoutBoundary(fixture, 10), End: fixturePayoutBoundary(fixture, 310), ProviderChunks: chunks, Extenders: committed,
		Root: [32]byte{0xab}, LedgerHash: fixture.header, RootSigner: fixture.versions[0].RootSigner, CommitBlock: 312}
	rows := DepositUsageRows{Count: 2, UsageBytes: 3 << 30}
	v2 := fixture.statementV2(t, nil)
	required := fixture.owedV2(t, entry, big.NewInt(0))
	if audit := EvaluateDepositUsage(v2, rows, expectation, 1, required, big.NewInt(0), policy); !audit.Compliant || audit.UsageEvidence != usage.EvidenceKindV2 || audit.UsageBytes != 3<<30+depositExtenderTestZ {
		t.Fatalf("v2: %+v", audit)
	}
	if audit := EvaluateDepositUsage(fixture.statement(t, nil), rows, expectation, 1, required, big.NewInt(0), policy); audit.Compliant || !strings.Contains(audit.Error, "a version 1 statement for an epoch with an extender component") {
		t.Fatalf("a v1 statement with a component: %+v", audit)
	}
	v1Expectation := *expectation
	v1Expectation.Extenders = nil
	if audit := EvaluateDepositUsage(v2, rows, &v1Expectation, 1, required, big.NewInt(0), policy); audit.Compliant || audit.UsageEvidence != usage.EvidenceKind || !strings.Contains(audit.Error, "a version 2 statement for an epoch without an extender component") {
		t.Fatalf("a v2 statement without a component: %+v", audit)
	}
	v1Required := fixture.owed(t, entry, big.NewInt(0))
	if audit := EvaluateDepositUsage(fixture.statement(t, nil), rows, &v1Expectation, 1, v1Required, big.NewInt(0), policy); !audit.Compliant || audit.UsageEvidence != usage.EvidenceKind || audit.UsageBytes != 3<<30 {
		t.Fatalf("v1: %+v", audit)
	}
	if audit := EvaluateDepositUsage(fixture.statement(t, func(s *usage.Statement) { s.TotalUsageBytes++ }), rows, &v1Expectation, 1, v1Required, big.NewInt(0), policy); audit.Error != "usage: the statement claims 3221225473 bytes in 2 rows; the ledger holds 3221225472 in 2" {
		t.Fatalf("the v1 message changed: %q", audit.Error)
	}
}

// The source epoch's boundary at block as an expectation names it.
func fixturePayoutBoundary(fixture *depositExtenderTestFixture, block uint64) payoutartifact.Boundary {
	return payoutartifact.Boundary{Number: block, Hash: releaseHex32(fixture.blocks[block])}
}
