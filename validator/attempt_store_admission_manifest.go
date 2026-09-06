//go:build linux || darwin

package validator

// CURRENT selects one strict manifest. Its streamed version edits retain only
// a bounded live table set; obsolete manifest history is never a replay map.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sort"

	"github.com/syndtr/goleveldb/leveldb/comparer"
	"github.com/syndtr/goleveldb/leveldb/journal"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

// Table bounds are copied metadata, never borrowed manifest buffers.
type attemptAdmissionTable struct {
	level   uint64
	number  int64
	size    uint64
	minimum []byte
	maximum []byte
}

// The sequence is native LevelDB sequence, distinct from attempt sequence.
type attemptAdmissionManifest struct {
	journal         int64
	previousJournal int64
	nextFile        int64
	sequence        uint64
	tableBytes      uint64
	tables          map[int64]attemptAdmissionTable
}

// The admitted physical ceiling and existing 4096-byte reservation prevent
// source level11 from reaching its exact native size threshold. Thus initial
// recovery and later compaction never score an overflowing level12 threshold.
const (
	attemptAdmissionHighestLevel        = uint64(11)
	attemptAdmissionMaximumStorageBytes = uint64(opt.DefaultCompactionTotalSize) * 100_000_000_000
)

// One bounded metadata field, checked before allocating its bytes.
func attemptAdmissionReadBytes(reader io.Reader, length uint64, limit uint64) ([]byte, error) {
	if length > limit || length > uint64(int(^uint(0)>>1)) {
		return nil, errors.New("attempt record store admission field exceeds its byte bound")
	}
	data := make([]byte, int(length))
	_, err := io.ReadFull(reader, data)
	return data, err
}

// Manifest file numbers are positive signed native descriptor numbers.
func attemptAdmissionNumber(reader io.ByteReader) (int64, error) {
	value, err := binary.ReadUvarint(reader)
	if err != nil || value > uint64(^uint64(0)>>1) {
		return 0, errors.Join(errors.New("attempt record store admission file number is invalid"), err)
	}
	return int64(value), nil
}

// Length-prefixed fields use the same binary primitive as the pinned decoder.
func attemptAdmissionManifestBytes(reader *bufio.Reader, limit uint64) ([]byte, error) {
	length, err := binary.ReadUvarint(reader)
	if err != nil {
		return nil, err
	}
	return attemptAdmissionReadBytes(reader, length, limit)
}

// Native replay indexes arrays by this value even though inspection does not.
func attemptAdmissionLevel(reader io.ByteReader) (uint64, error) {
	value, err := binary.ReadUvarint(reader)
	if err != nil || value > attemptAdmissionHighestLevel {
		return 0, errors.Join(errors.New("attempt record store admission level is outside the owned producer boundary"), err)
	}
	return value, nil
}

// Existing CURRENT absence is accepted only through the shared strict empty
// directory rule. No fallback locator or alternate manifest is selected.
func (self *attemptStoreAdmissionReader) manifest(keyLimit int) (result *attemptAdmissionManifest, resultErr error) {
	fd, err := self.disk.GetMeta()
	if errors.Is(err, os.ErrNotExist) {
		return nil, self.ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	file, err := self.disk.inspectFile(fd.String())
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close(), self.ctx.Err()) }()
	frames := journal.NewReader(io.NewSectionReader(file, 0, file.info.Size()), nil, true, true)
	reader := bufio.NewReaderSize(bytes.NewReader(nil), attemptAdmissionBuffer)
	manifest := &attemptAdmissionManifest{tables: map[int64]attemptAdmissionTable{}}
	var hasComparer, hasJournal, hasNext, hasSequence bool
	for {
		if err := self.ctx.Err(); err != nil {
			return nil, err
		}
		frame, err := frames.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		reader.Reset(frame)
		adds := make(map[int64]attemptAdmissionTable)
		deletes := make(map[int64]uint64)
		var compactPointers [attemptAdmissionHighestLevel + 1]bool
		for {
			if err := self.ctx.Err(); err != nil {
				return nil, err
			}
			tag, err := binary.ReadUvarint(reader)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			switch tag {
			case 1:
				name, err := attemptAdmissionManifestBytes(reader, uint64(len(comparer.DefaultComparer.Name())))
				if err != nil || string(name) != comparer.DefaultComparer.Name() {
					return nil, errors.Join(errors.New("attempt record store admission comparer differs"), err)
				}
				hasComparer = true
			case 2, 3, 9:
				number, err := attemptAdmissionNumber(reader)
				if err != nil {
					return nil, err
				}
				if tag == 2 {
					manifest.journal, hasJournal = number, true
				}
				if tag == 3 {
					manifest.nextFile, hasNext = number, true
				}
				if tag == 9 {
					manifest.previousJournal = number
				}
			case 4:
				sequence, err := binary.ReadUvarint(reader)
				if err != nil || sequence > attemptAdmissionMaxSequence {
					return nil, errors.Join(errors.New("attempt record store admission sequence is invalid"), err)
				}
				manifest.sequence, hasSequence = sequence, true
			case 5:
				level, err := attemptAdmissionLevel(reader)
				if err != nil {
					return nil, err
				}
				if compactPointers[level] {
					return nil, errors.New("attempt record store admission duplicate compaction pointer")
				}
				compactPointers[level] = true
				key, err := attemptAdmissionManifestBytes(reader, uint64(keyLimit))
				if err != nil {
					return nil, err
				}
				if _, err := attemptAdmissionCompare(key, key, true); err != nil {
					return nil, err
				}
			case 6, 7:
				level, err := attemptAdmissionLevel(reader)
				if err != nil {
					return nil, err
				}
				number, err := attemptAdmissionNumber(reader)
				if err != nil || number == 0 {
					return nil, errors.Join(errors.New("attempt record store admission table number is invalid"), err)
				}
				if tag == 6 {
					if _, exists := deletes[number]; exists {
						return nil, errors.New("attempt record store admission duplicate table deletion")
					}
					if _, exists := deletes[number]; !exists && uint64(len(deletes)) >= self.disk.bounds.MaxStorageFiles {
						return nil, errAttemptRecordStoreLimit
					}
					deletes[number] = level
				} else {
					if uint64(len(adds)) >= self.disk.bounds.MaxStorageFiles {
						return nil, errAttemptRecordStoreLimit
					}
					size, err := attemptAdmissionNumber(reader)
					if err != nil || size < 48 || uint64(size) > self.disk.bounds.MaxStorageBytes {
						return nil, errors.Join(errors.New("attempt record store admission table size is invalid"), err)
					}
					minimum, err := attemptAdmissionManifestBytes(reader, uint64(keyLimit))
					if err != nil {
						return nil, err
					}
					maximum, err := attemptAdmissionManifestBytes(reader, uint64(keyLimit))
					if err != nil {
						return nil, err
					}
					comparison, err := attemptAdmissionCompare(minimum, maximum, true)
					if err != nil || comparison > 0 {
						return nil, errors.Join(errors.New("attempt record store admission table range is invalid"), err)
					}
					if _, exists := adds[number]; exists {
						return nil, errors.New("attempt record store admission duplicate table addition")
					}
					adds[number] = attemptAdmissionTable{level: level, number: number, size: uint64(size), minimum: minimum, maximum: maximum}
				}
			default:
				return nil, errors.New("attempt record store admission manifest tag is unsupported")
			}
		}
		// Match native staging regardless of tag ordering inside this record.
		for number, level := range deletes {
			if table, exists := manifest.tables[number]; exists {
				if table.level != level {
					return nil, errors.New("attempt record store admission deleted table level differs")
				}
				manifest.tableBytes -= table.size
				delete(manifest.tables, number)
			}
		}
		for number, table := range adds {
			if _, exists := manifest.tables[number]; exists {
				return nil, errors.New("attempt record store admission live table is duplicated")
			}
			if uint64(len(manifest.tables)) >= self.disk.bounds.MaxStorageFiles {
				return nil, errAttemptRecordStoreLimit
			}
			// Check every committed version, including obsolete history, without
			// rescanning the complete live table set after each one-row edit.
			if table.size > uint64(^uint64(0)>>1)-manifest.tableBytes {
				return nil, errors.New("attempt record store admission native table sum overflows")
			}
			manifest.tableBytes += table.size
			manifest.tables[number] = table
		}
	}
	if !hasComparer || !hasJournal || !hasNext || !hasSequence {
		return nil, errors.New("attempt record store admission manifest authority is incomplete")
	}
	for _, table := range manifest.tables {
		name := (storage.FileDesc{Type: storage.TypeTable, Num: table.number}).String()
		info := self.disk.observed[name]
		if info == nil || uint64(info.Size()) != table.size || table.number >= manifest.nextFile {
			return nil, errors.New("attempt record store admission live table is missing or differs")
		}
	}
	return manifest, nil
}

// Checked canonical footer handles use the unchanged binary varint decoder.
func attemptAdmissionHandle(raw []byte) (attemptAdmissionBlockHandle, int, error) {
	offset, n := binary.Uvarint(raw)
	if n <= 0 {
		return attemptAdmissionBlockHandle{}, 0, errors.New("attempt record store admission block offset is invalid")
	}
	length, m := binary.Uvarint(raw[n:])
	if m <= 0 {
		return attemptAdmissionBlockHandle{}, 0, errors.New("attempt record store admission block length is invalid")
	}
	return attemptAdmissionBlockHandle{offset: offset, length: length}, n + m, nil
}

// Tables are scanned in native level order. The caller supplies exact row and
// metadata semantics; this layer validates complete physical framing and keys.
func (self *attemptStoreAdmissionReader) table(table attemptAdmissionTable, keyLimit int, valueLimit uint64, visit func([]byte, *io.LimitedReader) error) (resultErr error) {
	name := (storage.FileDesc{Type: storage.TypeTable, Num: table.number}).String()
	file, err := self.disk.inspectFile(name)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close(), self.ctx.Err()) }()
	if file.info.Size() < 48 {
		return errors.New("attempt record store admission table is short")
	}
	var footer [48]byte
	if _, err := file.ReadAt(footer[:], file.info.Size()-48); err != nil {
		return err
	}
	if string(footer[40:]) != "\x57\xfb\x80\x8b\x24\x75\x47\xdb" {
		return errors.New("attempt record store admission table magic differs")
	}
	meta, n, err := attemptAdmissionHandle(footer[:40])
	if err != nil {
		return err
	}
	index, m, err := attemptAdmissionHandle(footer[n:40])
	if err != nil {
		return err
	}
	for _, value := range footer[n+m : 40] {
		if value != 0 {
			return errors.New("attempt record store admission footer padding differs")
		}
	}
	footerOffset := uint64(file.info.Size() - 48)
	if meta.offset > footerOffset || meta.length > footerOffset-meta.offset || footerOffset-meta.offset-meta.length < 5 || index.offset != meta.offset+meta.length+5 || index.offset > footerOffset || index.length > footerOffset-index.offset || footerOffset-index.offset-index.length != 5 {
		return errors.New("attempt record store admission metadata block layout differs")
	}
	// These owned namespaces configure no filter; metadata must be empty.
	if err := self.walkBlock(file, meta, keyLimit, 20, false, func([]byte, *io.LimitedReader) error {
		return errors.New("attempt record store admission table filter metadata is unsupported")
	}); err != nil {
		return err
	}
	var nextOffset uint64
	var first, last, priorSeparator []byte
	err = self.walkBlock(file, index, keyLimit, 20, true, func(separator []byte, value *io.LimitedReader) error {
		var raw [20]byte
		length := int(value.N)
		if _, err := io.ReadFull(value, raw[:length]); err != nil {
			return err
		}
		handle, used, err := attemptAdmissionHandle(raw[:length])
		if err != nil || used != length || handle.offset != nextOffset || handle.offset > meta.offset || handle.length > meta.offset-handle.offset || meta.offset-handle.offset-handle.length < 5 {
			return errors.Join(errors.New("attempt record store admission data block layout differs"), err)
		}
		var blockLast []byte
		err = self.walkBlock(file, handle, keyLimit, valueLimit, true, func(key []byte, row *io.LimitedReader) error {
			if len(last) != 0 {
				comparison, err := attemptAdmissionCompare(last, key, true)
				if err != nil || comparison >= 0 {
					return errors.Join(errors.New("attempt record store admission cross-block order differs"), err)
				}
			}
			if len(blockLast) == 0 && len(priorSeparator) != 0 {
				comparison, err := attemptAdmissionCompare(priorSeparator, key, true)
				if err != nil || comparison >= 0 {
					return errors.Join(errors.New("attempt record store admission index separator overlaps next block"), err)
				}
			}
			if len(first) == 0 {
				first = bytes.Clone(key)
			}
			last = append(last[:0], key...)
			blockLast = append(blockLast[:0], key...)
			return visit(key, row)
		})
		if err != nil {
			return err
		}
		if len(blockLast) != 0 {
			comparison, err := attemptAdmissionCompare(blockLast, separator, true)
			if err != nil || comparison > 0 {
				return errors.Join(errors.New("attempt record store admission index separator precedes block"), err)
			}
		}
		priorSeparator = append(priorSeparator[:0], separator...)
		nextOffset = handle.offset + handle.length + 5
		return nil
	})
	if err != nil {
		return err
	}
	if nextOffset != meta.offset || !bytes.Equal(first, table.minimum) || !bytes.Equal(last, table.maximum) {
		return errors.New("attempt record store admission table census or range differs")
	}
	return nil
}

// A bounded detached order mirrors native level traversal, never map order.
func (self *attemptAdmissionManifest) orderedTables() []attemptAdmissionTable {
	tables := make([]attemptAdmissionTable, 0, len(self.tables))
	for _, table := range self.tables {
		tables = append(tables, table)
	}
	sort.Slice(tables, func(i, j int) bool {
		if tables[i].level != tables[j].level {
			return tables[i].level < tables[j].level
		}
		comparison, _ := attemptAdmissionCompare(tables[i].minimum, tables[j].minimum, true)
		if comparison != 0 {
			return comparison < 0
		}
		return tables[i].number < tables[j].number
	})
	return tables
}
