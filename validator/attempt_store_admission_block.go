//go:build linux || darwin

package validator

// Pinned snappy.Encode splits every input into independent 65536-byte slices
// before Go/assembly encoding. This reader accepts that owned producer's copy
// window, not arbitrary Snappy backreferences, and never allocates a full SST
// index or decoded block. Four reusable readers cover index/data restart joins.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"

	"github.com/syndtr/goleveldb/leveldb/util"
)

const (
	attemptAdmissionWindow      = 64 * 1024
	attemptAdmissionBuffer      = 32 * 1024
	attemptAdmissionMaxSequence = uint64(1<<56) - 1
)

// Only fixed working buffers and the caller's bounded file census survive a
// record. Pool counters support deterministic retained-memory regression tests.
type attemptStoreAdmissionReader struct {
	ctx           context.Context
	disk          *attemptRecordStoreStorage
	blocks        [4]attemptStoreAdmissionBlock
	scratch       [attemptAdmissionBuffer]byte
	activeBlocks  int
	maximumBlocks int
	decodedBytes  uint64
}

// Unsigned extents are checked before conversion to the native file offset.
type attemptAdmissionBlockHandle struct{ offset, length uint64 }

// Encoded bytes pass through the unchanged pinned CRC implementation.
type attemptAdmissionEncoded struct {
	reader io.Reader
	crc    util.CRC
}

// The section excludes the separately checked compression/checksum trailer.
func (self *attemptAdmissionEncoded) Read(data []byte) (int, error) {
	n, err := self.reader.Read(data)
	self.crc = self.crc.Update(data[:n])
	return n, err
}

// Block buffers are reused, not retained once per table or history version.
type attemptStoreAdmissionBlock struct {
	owner       *attemptStoreAdmissionReader
	busy        bool
	encoded     attemptAdmissionEncoded
	input       *bufio.Reader
	output      *bufio.Reader
	window      [attemptAdmissionWindow]byte
	length      uint64
	produced    uint64
	literal     uint64
	copyBytes   uint64
	copyOffset  uint64
	compression byte
	checksum    uint32
	finished    bool
}

// Reuses one bounded decoder only after its previous owner released it.
func (self *attemptStoreAdmissionReader) block(file *attemptStoreAdmissionFile, handle attemptAdmissionBlockHandle) (result *attemptStoreAdmissionBlock, resultErr error) {
	if err := self.ctx.Err(); err != nil {
		return nil, err
	}
	size := uint64(file.info.Size())
	if handle.offset > size || size-handle.offset < 5 || handle.length > size-handle.offset-5 {
		return nil, errors.New("attempt record store admission block extent is invalid")
	}
	var trailer [5]byte
	if _, err := file.ReadAt(trailer[:], int64(handle.offset+handle.length)); err != nil {
		return nil, err
	}
	if trailer[0] > 1 {
		return nil, errors.New("attempt record store admission compression is unsupported")
	}
	var block *attemptStoreAdmissionBlock
	for index := range self.blocks {
		if !self.blocks[index].busy {
			block = &self.blocks[index]
			break
		}
	}
	if block == nil {
		return nil, errors.New("attempt record store admission block ownership exceeded")
	}
	block.owner, block.busy = self, true
	self.activeBlocks++
	if self.activeBlocks > self.maximumBlocks {
		self.maximumBlocks = self.activeBlocks
	}
	defer func() {
		if resultErr != nil {
			block.release()
		}
	}()
	block.encoded = attemptAdmissionEncoded{reader: io.NewSectionReader(file, int64(handle.offset), int64(handle.length))}
	if block.input == nil {
		block.input = bufio.NewReaderSize(&block.encoded, attemptAdmissionBuffer)
	} else {
		block.input.Reset(&block.encoded)
	}
	block.produced, block.literal, block.copyBytes, block.copyOffset = 0, 0, 0, 0
	block.finished = false
	block.compression, block.checksum, block.length = trailer[0], binary.LittleEndian.Uint32(trailer[1:]), handle.length
	if block.compression == 1 {
		decoded, err := binary.ReadUvarint(block.input)
		if err != nil || decoded > uint64(^uint32(0)) {
			return nil, errors.Join(errors.New("attempt record store admission Snappy length is invalid"), err)
		}
		block.length = decoded
	}
	if block.output == nil {
		block.output = bufio.NewReaderSize(block, attemptAdmissionBuffer)
	} else {
		block.output.Reset(block)
	}
	return block, nil
}

// Release does not close the shared checked table descriptor.
func (self *attemptStoreAdmissionBlock) release() {
	if self.busy {
		self.busy = false
		self.owner.activeBlocks--
	}
}

// EOF is accepted only after exact decoded length, encoded EOF and full CRC.
func (self *attemptStoreAdmissionBlock) finish() error {
	if self.finished {
		return nil
	}
	if self.produced != self.length || self.literal != 0 || self.copyBytes != 0 {
		return errors.New("attempt record store admission block length differs")
	}
	if _, err := self.input.ReadByte(); err != io.EOF {
		return errors.Join(errors.New("attempt record store admission block has trailing encoded bytes"), err)
	}
	if self.encoded.crc.Update([]byte{self.compression}).Value() != self.checksum {
		return errors.New("attempt record store admission block checksum differs")
	}
	self.finished = true
	return self.owner.ctx.Err()
}

// All copy tags use the same checked window, including overlapping copies.
// An otherwise valid generic long-distance encoding is refused, not normalized.
func (self *attemptStoreAdmissionBlock) nextTag() error {
	tag, err := self.input.ReadByte()
	if err != nil {
		return err
	}
	if tag&3 == 0 {
		length := uint64(tag >> 2)
		if length >= 60 {
			count := int(length - 59)
			var raw [4]byte
			if _, err := io.ReadFull(self.input, raw[:count]); err != nil {
				return err
			}
			length = uint64(binary.LittleEndian.Uint32(raw[:]))
		}
		self.literal = length + 1
		if self.literal > self.length-self.produced {
			return errors.New("attempt record store admission Snappy literal exceeds decoded block")
		}
		return nil
	}
	var raw [4]byte
	switch tag & 3 {
	case 1:
		if _, err := io.ReadFull(self.input, raw[:1]); err != nil {
			return err
		}
		self.copyBytes, self.copyOffset = 4+uint64((tag>>2)&7), uint64(tag&0xe0)<<3|uint64(raw[0])
	case 2:
		if _, err := io.ReadFull(self.input, raw[:2]); err != nil {
			return err
		}
		self.copyBytes, self.copyOffset = 1+uint64(tag>>2), uint64(binary.LittleEndian.Uint16(raw[:2]))
	case 3:
		if _, err := io.ReadFull(self.input, raw[:]); err != nil {
			return err
		}
		self.copyBytes, self.copyOffset = 1+uint64(tag>>2), uint64(binary.LittleEndian.Uint32(raw[:]))
	}
	if self.copyOffset >= attemptAdmissionWindow {
		return errors.New("attempt record store admission unsupported Snappy copy distance")
	}
	if self.copyOffset == 0 || self.copyOffset > self.produced || self.copyBytes > self.length-self.produced {
		return errors.New("attempt record store admission Snappy copy is invalid")
	}
	return nil
}

// Context checkpoints bound literal and copy work even for enormous indexes.
func (self *attemptStoreAdmissionBlock) Read(data []byte) (int, error) {
	if !self.busy {
		return 0, errors.New("attempt record store admission block is released")
	}
	if err := self.owner.ctx.Err(); err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	if self.produced == self.length {
		if err := self.finish(); err != nil {
			return 0, err
		}
		return 0, io.EOF
	}
	n := 0
	for n < len(data) && self.produced < self.length {
		if err := self.owner.ctx.Err(); err != nil {
			return n, err
		}
		take := min(uint64(len(data)-n), self.length-self.produced, uint64(attemptAdmissionBuffer))
		if self.compression != 0 {
			if self.literal == 0 && self.copyBytes == 0 {
				if err := self.nextTag(); err != nil {
					return n, err
				}
			}
		}
		var readErr error
		if self.compression == 0 || self.literal != 0 {
			if self.compression != 0 {
				take = min(take, self.literal)
			}
			copied, err := io.ReadFull(self.input, data[n:n+int(take)])
			take, readErr = uint64(copied), err
			if self.compression != 0 {
				self.literal -= take
			}
		} else {
			take = min(take, self.copyBytes)
			seed := min(take, self.copyOffset)
			start := (self.produced - self.copyOffset) % attemptAdmissionWindow
			first := min(seed, attemptAdmissionWindow-start)
			copy(data[n:n+int(first)], self.window[int(start):int(start+first)])
			copy(data[n+int(first):n+int(seed)], self.window[:int(seed-first)])
			// Copy the already produced period, doubling as needed for an
			// overlapping backreference. No byte-at-a-time history loop.
			for filled := seed; filled < take; {
				filled += uint64(copy(data[n+int(filled):n+int(take)], data[n:n+int(filled)]))
			}
			self.copyBytes -= take
		}
		start := self.produced % attemptAdmissionWindow
		first := min(take, attemptAdmissionWindow-start)
		copy(self.window[int(start):int(start+first)], data[n:n+int(first)])
		copy(self.window[:int(take-first)], data[n+int(first):n+int(take)])
		self.produced += take
		self.owner.decodedBytes += take
		n += int(take)
		if readErr != nil {
			return n, readErr
		}
	}
	return n, nil
}

// Skips through the actual decoder using fixed scratch, never io.ReadAll.
func attemptAdmissionSkip(reader io.Reader, count uint64, scratch []byte) error {
	if count != 0 && len(scratch) == 0 {
		return errors.New("attempt record store admission skip buffer is absent")
	}
	for count != 0 {
		size := uint64(len(scratch))
		if count < size {
			size = count
		}
		if _, err := io.ReadFull(reader, scratch[:int(size)]); err != nil {
			return err
		}
		count -= size
	}
	return nil
}

// Reuses binary's overflow validation while counting exact entry framing.
type attemptAdmissionCountingReader struct {
	reader *bufio.Reader
	offset uint64
}

func (self *attemptAdmissionCountingReader) ReadByte() (byte, error) {
	value, err := self.reader.ReadByte()
	if err == nil {
		self.offset++
	}
	return value, err
}

func (self *attemptAdmissionCountingReader) Read(data []byte) (int, error) {
	n, err := self.reader.Read(data)
	self.offset += uint64(n)
	return n, err
}

// Table comparators order user keys ascending and their sequence/type suffix
// descending. Reject malformed suffixes instead of invoking native panics.
func attemptAdmissionCompare(left, right []byte, internal bool) (int, error) {
	if !internal {
		return bytes.Compare(left, right), nil
	}
	if len(left) < 8 || len(right) < 8 || left[len(left)-8] > 1 || right[len(right)-8] > 1 {
		return 0, errors.New("attempt record store admission internal key is invalid")
	}
	comparison := bytes.Compare(left[:len(left)-8], right[:len(right)-8])
	if comparison != 0 {
		return comparison, nil
	}
	a, b := binary.LittleEndian.Uint64(left[len(left)-8:]), binary.LittleEndian.Uint64(right[len(right)-8:])
	if a > b {
		return -1, nil
	}
	if a < b {
		return 1, nil
	}
	return 0, nil
}

// Restarts and entry positions are merged as two streams. The initial footer
// pass obtains only the count; no index-sized restart slice is allocated.
func (self *attemptStoreAdmissionReader) walkBlock(file *attemptStoreAdmissionFile, handle attemptAdmissionBlockHandle, keyLimit int, valueLimit uint64, internal bool, visit func([]byte, *io.LimitedReader) error) error {
	probe, err := self.block(file, handle)
	if err != nil {
		return err
	}
	length := probe.length
	var raw [4]byte
	if length < 8 {
		probe.release()
		return errors.New("attempt record store admission block footer is short")
	}
	err = attemptAdmissionSkip(probe.output, length-4, self.scratch[:])
	if err == nil {
		_, err = io.ReadFull(probe.output, raw[:])
	}
	if err == nil {
		err = probe.finish()
	}
	probe.release()
	if err != nil {
		return err
	}
	count := uint64(binary.LittleEndian.Uint32(raw[:]))
	if count == 0 || count > (length-4)/4 {
		return errors.New("attempt record store admission restart count is invalid")
	}
	end := length - (count+1)*4
	restarts, err := self.block(file, handle)
	if err != nil {
		return err
	}
	defer restarts.release()
	if err := attemptAdmissionSkip(restarts.output, end, self.scratch[:]); err != nil {
		return err
	}
	if _, err := io.ReadFull(restarts.output, raw[:]); err != nil {
		return err
	}
	nextRestart := uint64(binary.LittleEndian.Uint32(raw[:]))
	if nextRestart != 0 {
		return errors.New("attempt record store admission first restart is not zero")
	}
	entries, err := self.block(file, handle)
	if err != nil {
		return err
	}
	defer entries.release()
	reader := &attemptAdmissionCountingReader{reader: entries.output}
	key := make([]byte, 0, keyLimit)
	previous := make([]byte, 0, keyLimit)
	var usedRestarts, records uint64
	for reader.offset < end {
		start := reader.offset
		shared, err := binary.ReadUvarint(reader)
		if err != nil {
			return err
		}
		fresh, err := binary.ReadUvarint(reader)
		if err != nil {
			return err
		}
		valueSize, err := binary.ReadUvarint(reader)
		if err != nil {
			return err
		}
		if shared > uint64(len(key)) || shared > uint64(keyLimit) || fresh > uint64(keyLimit)-shared || valueSize > valueLimit || reader.offset > end || fresh > end-reader.offset || valueSize > end-reader.offset-fresh {
			return errors.New("attempt record store admission entry exceeds its typed bounds")
		}
		if usedRestarts < count {
			if nextRestart < start {
				return errors.New("attempt record store admission restart is not an entry")
			}
			if nextRestart == start {
				if shared != 0 {
					return errors.New("attempt record store admission restart shares a prefix")
				}
				usedRestarts++
				if usedRestarts < count {
					if _, err := io.ReadFull(restarts.output, raw[:]); err != nil {
						return err
					}
					next := uint64(binary.LittleEndian.Uint32(raw[:]))
					if next <= nextRestart || next >= end {
						return errors.New("attempt record store admission restart offsets differ")
					}
					nextRestart = next
				}
			}
		}
		key = key[:int(shared+fresh)]
		if _, err := io.ReadFull(reader, key[int(shared):]); err != nil {
			return err
		}
		if internal {
			if _, err := attemptAdmissionCompare(key, key, true); err != nil {
				return err
			}
		}
		if records != 0 {
			comparison, err := attemptAdmissionCompare(previous, key, internal)
			if err != nil || comparison >= 0 {
				return errors.Join(errors.New("attempt record store admission keys are not ordered"), err)
			}
		}
		previous = append(previous[:0], key...)
		value := &io.LimitedReader{R: reader, N: int64(valueSize)}
		if visit != nil {
			if err := visit(key, value); err != nil {
				return err
			}
		}
		if err := attemptAdmissionSkip(value, uint64(value.N), self.scratch[:]); err != nil {
			return err
		}
		records++
	}
	if records == 0 && count == 1 && end == 0 {
		usedRestarts = 1
	}
	if reader.offset != end || usedRestarts != count {
		return errors.New("attempt record store admission restart census differs")
	}
	if _, err := io.ReadFull(restarts.output, raw[:]); err != nil {
		return err
	}
	if uint64(binary.LittleEndian.Uint32(raw[:])) != count {
		return errors.New("attempt record store admission restart footer changed")
	}
	if err := restarts.finish(); err != nil {
		return err
	}
	if err := attemptAdmissionSkip(entries.output, length-end, self.scratch[:]); err != nil {
		return err
	}
	return entries.finish()
}
