package solana

import (
	"crypto/ed25519"
	"errors"
	"fmt"
)

// Transaction is a parsed legacy or v0 transaction: the signature slots and
// the exact message bytes they sign. Signing never re-encodes the message, so
// what Jupiter built is exactly what gets signed.
type Transaction struct {
	Signatures            [][64]byte
	Message               []byte
	Version               int // -1 for legacy, 0 for v0
	NumRequiredSignatures int
	StaticKeys            []PublicKey
	RecentBlockhash       [32]byte

	programIndexes []int
}

// ParseTransaction decodes the wire format
//
//	compact-u16 n | n × 64-byte signatures | message
//
// where a v0 message starts with the prefix 0x80. Legacy and v0 are accepted.
// Anything else is refused, including Solana's version-prefixed v1 format,
// whose first byte is 0x81 and whose signatures trail the message.
func ParseTransaction(raw []byte) (*Transaction, error) {
	if len(raw) == 0 {
		return nil, errors.New("solana: empty transaction")
	}
	if raw[0]&0x80 != 0 {
		return nil, fmt.Errorf("solana: unsupported transaction format (prefix 0x%02x); only legacy and v0 are accepted", raw[0])
	}
	r := &reader{b: raw}
	n := r.compactU16()
	signatures := make([][64]byte, n)
	for i := range signatures {
		copy(signatures[i][:], r.take(64))
	}
	if r.err != nil {
		return nil, r.err
	}
	t := &Transaction{Signatures: signatures, Message: raw[r.i:]}
	if err := t.parseMessage(); err != nil {
		return nil, err
	}
	if len(signatures) != t.NumRequiredSignatures {
		return nil, fmt.Errorf("solana: %d signature slots for %d required signers", len(signatures), t.NumRequiredSignatures)
	}
	return t, nil
}

func (t *Transaction) parseMessage() error {
	r := &reader{b: t.Message}
	t.Version = -1
	if len(t.Message) > 0 && t.Message[0]&0x80 != 0 {
		version := int(t.Message[0] & 0x7f)
		if version != 0 {
			return fmt.Errorf("solana: unsupported message version %d", version)
		}
		t.Version = 0
		r.i = 1
	}
	header := r.take(3)
	keyCount := r.compactU16()
	keys := make([]PublicKey, keyCount)
	for i := range keys {
		copy(keys[i][:], r.take(32))
	}
	copy(t.RecentBlockhash[:], r.take(32))
	instructions := r.compactU16()
	t.programIndexes = make([]int, 0, instructions)
	for range instructions {
		program := r.take(1)
		r.take(r.compactU16()) // account indexes
		r.take(r.compactU16()) // data
		if len(program) == 1 {
			t.programIndexes = append(t.programIndexes, int(program[0]))
		}
	}
	if t.Version == 0 {
		lookups := r.compactU16()
		for range lookups {
			r.take(32)
			r.take(r.compactU16()) // writable indexes
			r.take(r.compactU16()) // readonly indexes
		}
	}
	if r.err != nil {
		return r.err
	}
	if r.i != len(t.Message) {
		return fmt.Errorf("solana: %d trailing bytes after the message", len(t.Message)-r.i)
	}
	t.NumRequiredSignatures = int(header[0])
	t.StaticKeys = keys
	if t.NumRequiredSignatures == 0 || t.NumRequiredSignatures > len(keys) {
		return fmt.Errorf("solana: %d required signers for %d static keys", t.NumRequiredSignatures, len(keys))
	}
	return nil
}

// RequiredSigners are the static keys whose signatures the message needs, in
// slot order; the first is the fee payer.
func (t *Transaction) RequiredSigners() []PublicKey {
	return t.StaticKeys[:t.NumRequiredSignatures]
}

// FeePayer is the first required signer.
func (t *Transaction) FeePayer() PublicKey { return t.StaticKeys[0] }

// ProgramIDs lists the program invoked by each top-level instruction. ok is
// false when a program index points past the static keys (into an address
// lookup table), so the list cannot be trusted as complete.
func (t *Transaction) ProgramIDs() (programs []PublicKey, ok bool) {
	ok = true
	for _, index := range t.programIndexes {
		if index >= len(t.StaticKeys) {
			ok = false
			continue
		}
		programs = append(programs, t.StaticKeys[index])
	}
	return programs, ok
}

func (t *Transaction) slot(pub PublicKey) int {
	for i, signer := range t.RequiredSigners() {
		if signer == pub {
			return i
		}
	}
	return -1
}

// Sign writes key's signature over the message into its own slot. It fails if
// key is not a required signer; other slots are left as they are.
func (t *Transaction) Sign(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return errors.New("solana: not an ed25519 private key")
	}
	pub := PublicKeyOf(key)
	i := t.slot(pub)
	if i < 0 {
		return fmt.Errorf("solana: %s is not a required signer of this transaction", pub)
	}
	copy(t.Signatures[i][:], ed25519.Sign(key, t.Message))
	return nil
}

// SignedBy reports whether pub's slot holds a valid signature of the message.
func (t *Transaction) SignedBy(pub PublicKey) bool {
	i := t.slot(pub)
	return i >= 0 && ed25519.Verify(ed25519.PublicKey(pub[:]), t.Message, t.Signatures[i][:])
}

// Serialize is the wire format ParseTransaction reads.
func (t *Transaction) Serialize() []byte {
	out := appendCompactU16(nil, len(t.Signatures))
	for _, signature := range t.Signatures {
		out = append(out, signature[:]...)
	}
	return append(out, t.Message...)
}

// ID is the transaction's signature as RPCs and explorers name it: the base58
// of the fee payer's signature.
func (t *Transaction) ID() string {
	if len(t.Signatures) == 0 {
		return ""
	}
	return Base58Encode(t.Signatures[0][:])
}

type reader struct {
	b   []byte
	i   int
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.i+n > len(r.b) {
		r.err = errors.New("solana: truncated transaction")
		return nil
	}
	out := r.b[r.i : r.i+n]
	r.i += n
	return out
}

// compactU16 reads Solana's shortvec: 7 bits per byte, little-endian, at most
// three bytes.
func (r *reader) compactU16() int {
	value := 0
	for shift := 0; shift < 21; shift += 7 {
		b := r.take(1)
		if b == nil {
			return 0
		}
		value |= int(b[0]&0x7f) << shift
		if b[0]&0x80 == 0 {
			if value > 0xffff {
				r.err = errors.New("solana: compact-u16 overflow")
				return 0
			}
			return value
		}
	}
	r.err = errors.New("solana: compact-u16 longer than 3 bytes")
	return 0
}

func appendCompactU16(out []byte, n int) []byte {
	for {
		b := byte(n & 0x7f)
		n >>= 7
		if n == 0 {
			return append(out, b)
		}
		out = append(out, b|0x80)
	}
}

// Instruction is one instruction to compile into a transaction.
type Instruction struct {
	Program  PublicKey
	Accounts []AccountMeta
	Data     []byte
}

// AccountMeta is one account of an instruction.
type AccountMeta struct {
	Key      PublicKey
	Signer   bool
	Writable bool
}

// LookupTable is an address lookup table's address and contents.
type LookupTable struct {
	Address   PublicKey
	Addresses []PublicKey
}

// lookupTableHeader is the bytes before an address lookup table's addresses
// (LOOKUP_TABLE_META_SIZE).
const lookupTableHeader = 56

// ParseLookupTable decodes an address lookup table account's data.
func ParseLookupTable(address PublicKey, data []byte) (*LookupTable, error) {
	if len(data) < lookupTableHeader || (len(data)-lookupTableHeader)%32 != 0 {
		return nil, fmt.Errorf("solana: %s is not an address lookup table (%d bytes)", address, len(data))
	}
	table := &LookupTable{Address: address, Addresses: make([]PublicKey, (len(data)-lookupTableHeader)/32)}
	for i := range table.Addresses {
		copy(table.Addresses[i][:], data[lookupTableHeader+i*32:])
	}
	return table, nil
}

// CompileV0 builds an unsigned v0 transaction paid by payer. Accounts found in a
// table leave the static key list, except signers, the payer and the programs
// instructions invoke, which must stay static. The signature slots are zero.
func CompileV0(payer PublicKey, instructions []Instruction, blockhash [32]byte, tables []*LookupTable) (*Transaction, error) {
	type flags struct{ signer, writable, program bool }
	seen := map[PublicKey]*flags{payer: {signer: true, writable: true}}
	order := []PublicKey{payer}
	mark := func(key PublicKey, signer, writable, program bool) {
		f := seen[key]
		if f == nil {
			f = &flags{}
			seen[key] = f
			order = append(order, key)
		}
		f.signer, f.writable, f.program = f.signer || signer, f.writable || writable, f.program || program
	}
	for _, ix := range instructions {
		mark(ix.Program, false, false, true)
		for _, account := range ix.Accounts {
			mark(account.Key, account.Signer, account.Writable, false)
		}
	}
	type lookup struct {
		table, index int
	}
	found := map[PublicKey]lookup{}
	for t, table := range tables {
		for i, address := range table.Addresses {
			if _, ok := found[address]; !ok && i < 256 {
				found[address] = lookup{t, i}
			}
		}
	}
	var writableSigners, readonlySigners, writableStatic, readonlyStatic []PublicKey
	loadedWritable := make([][]PublicKey, len(tables))
	loadedReadonly := make([][]PublicKey, len(tables))
	writableIndexes := make([][]byte, len(tables))
	readonlyIndexes := make([][]byte, len(tables))
	for _, key := range order {
		f := seen[key]
		at, inTable := found[key]
		switch {
		case f.signer && f.writable:
			writableSigners = append(writableSigners, key)
		case f.signer:
			readonlySigners = append(readonlySigners, key)
		case inTable && !f.program && f.writable:
			loadedWritable[at.table] = append(loadedWritable[at.table], key)
			writableIndexes[at.table] = append(writableIndexes[at.table], byte(at.index))
		case inTable && !f.program:
			loadedReadonly[at.table] = append(loadedReadonly[at.table], key)
			readonlyIndexes[at.table] = append(readonlyIndexes[at.table], byte(at.index))
		case f.writable:
			writableStatic = append(writableStatic, key)
		default:
			readonlyStatic = append(readonlyStatic, key)
		}
	}
	static := append(append(append(append([]PublicKey{}, writableSigners...), readonlySigners...), writableStatic...), readonlyStatic...)
	// The account index space: the static keys, then every table's writable
	// loads, then every table's read-only loads.
	indexes := make(map[PublicKey]int, len(order))
	next := 0
	for _, key := range static {
		indexes[key] = next
		next++
	}
	for _, keys := range loadedWritable {
		for _, key := range keys {
			indexes[key] = next
			next++
		}
	}
	for _, keys := range loadedReadonly {
		for _, key := range keys {
			indexes[key] = next
			next++
		}
	}
	if next > 256 {
		return nil, fmt.Errorf("solana: %d accounts exceed a transaction's 256", next)
	}
	message := []byte{0x80, byte(len(writableSigners) + len(readonlySigners)), byte(len(readonlySigners)), byte(len(readonlyStatic))}
	message = appendCompactU16(message, len(static))
	for _, key := range static {
		message = append(message, key[:]...)
	}
	message = append(message, blockhash[:]...)
	message = appendCompactU16(message, len(instructions))
	for _, ix := range instructions {
		message = append(message, byte(indexes[ix.Program]))
		message = appendCompactU16(message, len(ix.Accounts))
		for _, account := range ix.Accounts {
			message = append(message, byte(indexes[account.Key]))
		}
		message = appendCompactU16(message, len(ix.Data))
		message = append(message, ix.Data...)
	}
	used := 0
	for t := range tables {
		if len(writableIndexes[t])+len(readonlyIndexes[t]) > 0 {
			used++
		}
	}
	message = appendCompactU16(message, used)
	for t, table := range tables {
		if len(writableIndexes[t])+len(readonlyIndexes[t]) == 0 {
			continue
		}
		message = append(message, table.Address[:]...)
		message = appendCompactU16(message, len(writableIndexes[t]))
		message = append(message, writableIndexes[t]...)
		message = appendCompactU16(message, len(readonlyIndexes[t]))
		message = append(message, readonlyIndexes[t]...)
	}
	required := len(writableSigners) + len(readonlySigners)
	raw := appendCompactU16(nil, required)
	raw = append(raw, make([]byte, required*64)...)
	raw = append(raw, message...)
	// A transaction larger than a packet never lands; refuse it here rather
	// than at the RPC.
	if len(raw) > MaxTransactionBytes {
		return nil, fmt.Errorf("solana: compiled transaction of %d bytes exceeds %d", len(raw), MaxTransactionBytes)
	}
	return ParseTransaction(raw)
}

// ResolveInstructions reads a compiled v0 message back into instructions, through
// its lookup tables: what the transaction would really touch, with each account's
// signer and writable flags as the runtime sees them.
func (t *Transaction) ResolveInstructions(tables []*LookupTable) ([]Instruction, error) {
	r := &reader{b: t.Message}
	if t.Version == 0 {
		r.i = 1
	}
	header := r.take(3)
	r.take(r.compactU16() * 32)
	r.take(32)
	type compiled struct {
		program int
		indexes []byte
		data    []byte
	}
	instructions := make([]compiled, r.compactU16())
	for i := range instructions {
		instructions[i].program = int(r.take(1)[0])
		instructions[i].indexes = r.take(r.compactU16())
		instructions[i].data = r.take(r.compactU16())
	}
	var writable, readonly []PublicKey
	if t.Version == 0 {
		for range r.compactU16() {
			address := PublicKey(r.take(32))
			var table *LookupTable
			for _, candidate := range tables {
				if candidate.Address == address {
					table = candidate
				}
			}
			if table == nil {
				return nil, fmt.Errorf("solana: no lookup table %s", address)
			}
			load := func(into *[]PublicKey, indexes []byte) error {
				for _, index := range indexes {
					if int(index) >= len(table.Addresses) {
						return fmt.Errorf("solana: index %d past lookup table %s", index, address)
					}
					*into = append(*into, table.Addresses[index])
				}
				return nil
			}
			if err := load(&writable, r.take(r.compactU16())); err != nil {
				return nil, err
			}
			if err := load(&readonly, r.take(r.compactU16())); err != nil {
				return nil, err
			}
		}
	}
	if r.err != nil {
		return nil, r.err
	}
	static := len(t.StaticKeys)
	signers, readonlySigners, readonlyUnsigned := int(header[0]), int(header[1]), int(header[2])
	keys := append(append(append([]PublicKey{}, t.StaticKeys...), writable...), readonly...)
	isWritable := func(i int) bool {
		switch {
		case i < signers:
			return i < signers-readonlySigners
		case i < static:
			return i < static-readonlyUnsigned
		}
		return i < static+len(writable)
	}
	out := make([]Instruction, len(instructions))
	for i, instruction := range instructions {
		if instruction.program >= len(keys) {
			return nil, errors.New("solana: program index out of range")
		}
		out[i] = Instruction{Program: keys[instruction.program], Data: instruction.data, Accounts: make([]AccountMeta, len(instruction.indexes))}
		for j, index := range instruction.indexes {
			if int(index) >= len(keys) {
				return nil, errors.New("solana: account index out of range")
			}
			out[i].Accounts[j] = AccountMeta{Key: keys[index], Signer: int(index) < signers, Writable: isWritable(int(index))}
		}
	}
	return out, nil
}

// MaxTransactionBytes is Solana's packet limit for a transaction.
const MaxTransactionBytes = 1232
