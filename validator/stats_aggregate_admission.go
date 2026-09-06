//go:build linux || darwin

package validator

// Aggregate admission reads only the existing private authority. It does not
// replay providers into RAM or mutate recovery rows. Native writable Open runs
// afterwards under the same retained directory gate and must agree exactly.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sort"

	"github.com/syndtr/goleveldb/leveldb/journal"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

// These are allocated buffer capacities, not a runtime heap/RSS estimate.
type statsAggregateAdmissionReport struct {
	BlockSlots          int
	MaximumActiveBlocks int
	WindowBytes         int
	EncodedBufferBytes  int
	DecodedBufferBytes  int
	ScratchBufferBytes  int
	DecodedBytes        uint64
}

// Native sequence selects a physical version; the header carries independent
// attempt/settlement/native clocks and complete immutable namespace context.
type statsAggregateAdmissionHead struct {
	found    bool
	sequence uint64
	head     statsAggregateHead
}

// Only the existing fixed row shapes and bounded canonical h value are valid.
func statsAggregateAdmissionValueLimit(key []byte, kind byte, bounds statsAggregateBounds) (uint64, error) {
	if len(key) == 0 || kind > 1 {
		return 0, errors.New("private aggregate admission key is invalid")
	}
	if bytes.Equal(key, []byte{'h'}) {
		if kind == 0 {
			return 0, errors.New("private aggregate admission authority was deleted")
		}
		return bounds.MaxHeaderBytes, nil
	}
	if _, err := statsAggregateBatchRecordBytes(key, nil, true); err != nil {
		return 0, errors.Join(errors.New("private aggregate contains an unknown row"), err)
	}
	if kind == 0 || key[0] == 'e' {
		return 0, nil
	}
	if key[0] == 'p' {
		return statsAggregateProviderBytes, nil
	}
	return statsAggregateClaimBytes, nil
}

// Canonical decoding retains every Config field; policy PPM is not a substitute
// for the separately persisted reporting transform or an advanced clock.
func statsAggregateReadAdmissionHead(value io.Reader, size uint64, sequence uint64, initial statsAggregateHead, bounds statsAggregateBounds) (statsAggregateAdmissionHead, error) {
	raw, err := attemptAdmissionReadBytes(value, size, bounds.MaxHeaderBytes)
	if err != nil {
		return statsAggregateAdmissionHead{}, err
	}
	var head statsAggregateHead
	if err := attemptStoreDecode(raw, &head); err != nil {
		return statsAggregateAdmissionHead{}, err
	}
	if err := head.validate(bounds); err != nil {
		return statsAggregateAdmissionHead{}, err
	}
	if head.Identity != initial.Identity || head.Activation != initial.Activation || head.Config != initial.Config {
		return statsAggregateAdmissionHead{}, errors.New("private aggregate existing namespace or config differs")
	}
	return statsAggregateAdmissionHead{found: true, sequence: sequence, head: head}, nil
}

// Equal native sequence must not route to different committed private heads.
func statsAggregateNewestAdmissionHead(prior, next statsAggregateAdmissionHead) (statsAggregateAdmissionHead, error) {
	if !next.found {
		return prior, nil
	}
	if !prior.found || next.sequence > prior.sequence {
		return next, nil
	}
	if next.sequence == prior.sequence && next.head != prior.head {
		return prior, errors.New("private aggregate admission equal native sequences disagree")
	}
	return prior, nil
}

// Metadata selection mirrors native L0 overlap, higher-level order and strict
// selected-WAL replay. All retained h versions must share one fixed namespace.
func (self *statsAggregateStore) inspectHead(ctx context.Context, initial statsAggregateHead) (result *statsAggregateHead, resultErr error) {
	reader := &attemptStoreAdmissionReader{ctx: ctx, disk: self.disk}
	manifest, err := reader.manifest(81)
	if err != nil {
		return nil, err
	}
	var selected statsAggregateAdmissionHead
	var selectedLevel, maximumSequence uint64
	rowsSeen := false
	if manifest != nil {
		valueLimit := self.bounds.MaxHeaderBytes
		if valueLimit < statsAggregateProviderBytes {
			valueLimit = statsAggregateProviderBytes
		}
		if valueLimit < statsAggregateClaimBytes {
			valueLimit = statsAggregateClaimBytes
		}
		var previous *attemptAdmissionTable
		for _, table := range manifest.orderedTables() {
			if previous != nil && table.level != 0 && table.level == previous.level && bytes.Compare(previous.maximum[:len(previous.maximum)-8], table.minimum[:len(table.minimum)-8]) >= 0 {
				return nil, errors.New("private aggregate admission higher-level tables overlap")
			}
			var tableHead statsAggregateAdmissionHead
			err := reader.table(table, 81, valueLimit, func(key []byte, value *io.LimitedReader) error {
				native := binary.LittleEndian.Uint64(key[len(key)-8:])
				sequence, kind, userKey := native>>8, byte(native&255), key[:len(key)-8]
				if sequence > maximumSequence {
					maximumSequence = sequence
				}
				limit, err := statsAggregateAdmissionValueLimit(userKey, kind, self.bounds)
				if err != nil {
					return err
				}
				if bytes.Equal(userKey, []byte{'h'}) {
					if uint64(value.N) > limit {
						return errors.New("private aggregate admission header is oversized")
					}
					next, err := statsAggregateReadAdmissionHead(value, uint64(value.N), sequence, initial, self.bounds)
					if err != nil {
						return err
					}
					tableHead, err = statsAggregateNewestAdmissionHead(tableHead, next)
					return err
				}
				if uint64(value.N) != limit {
					return errors.New("private aggregate admission row shape differs")
				}
				rowsSeen = true
				return nil
			})
			if err != nil {
				return nil, err
			}
			if tableHead.found && (!selected.found || table.level == 0 && selectedLevel == 0) {
				selected, err = statsAggregateNewestAdmissionHead(selected, tableHead)
				if err != nil {
					return nil, err
				}
				selectedLevel = table.level
			}
			copy := table
			previous = &copy
		}
		sequence := manifest.sequence
		journals, err := self.disk.List(storage.TypeJournal)
		if err != nil {
			return nil, err
		}
		sort.Slice(journals, func(i, j int) bool { return journals[i].Num < journals[j].Num })
		for _, fd := range journals {
			if fd.Num < manifest.journal && fd.Num != manifest.previousJournal {
				continue
			}
			next, nextSequence, rows, err := self.inspectJournal(ctx, fd, sequence, initial, reader.scratch[:])
			if err != nil {
				return nil, err
			}
			sequence, rowsSeen = nextSequence, rowsSeen || rows
			selected, err = statsAggregateNewestAdmissionHead(selected, next)
			if err != nil {
				return nil, err
			}
		}
		if maximumSequence > sequence {
			return nil, errors.New("private aggregate admission table sequence exceeds recovered clock")
		}
	}
	if !selected.found && rowsSeen {
		return nil, errors.New("private aggregate has rows without an authority header")
	}
	report := statsAggregateAdmissionReport{BlockSlots: len(reader.blocks), MaximumActiveBlocks: reader.maximumBlocks, WindowBytes: len(reader.blocks) * attemptAdmissionWindow, ScratchBufferBytes: len(reader.scratch), DecodedBytes: reader.decodedBytes}
	for index := range reader.blocks {
		if reader.blocks[index].input != nil {
			report.EncodedBufferBytes += reader.blocks[index].input.Size()
		}
		if reader.blocks[index].output != nil {
			report.DecodedBufferBytes += reader.blocks[index].output.Size()
		}
	}
	if self.hooks.AdmissionChecked != nil {
		if err := self.hooks.AdmissionChecked(report); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if selected.found {
		return &selected.head, nil
	}
	return nil, nil
}

// A complete bounded batch is staged before its h candidate is published.
// Strict journal framing/checksum/torn-tail behavior is the pinned native reader.
func (self *statsAggregateStore) inspectJournal(ctx context.Context, fd storage.FileDesc, sequence uint64, initial statsAggregateHead, scratch []byte) (head statsAggregateAdmissionHead, nextSequence uint64, rows bool, resultErr error) {
	file, err := self.disk.inspectFile(fd.String())
	if err != nil {
		return head, sequence, false, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close(), ctx.Err()) }()
	frames := journal.NewReader(io.NewSectionReader(file, 0, file.info.Size()), nil, true, true)
	buffer := bufio.NewReaderSize(bytes.NewReader(nil), attemptAdmissionBuffer)
	for {
		if err := ctx.Err(); err != nil {
			return head, sequence, rows, err
		}
		frame, err := frames.Next()
		if errors.Is(err, io.EOF) {
			return head, sequence, rows, nil
		}
		if err != nil {
			return head, sequence, rows, err
		}
		buffer.Reset(frame)
		reader := &attemptAdmissionCountingReader{reader: buffer}
		var header [12]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			return head, sequence, rows, err
		}
		start, count := binary.LittleEndian.Uint64(header[:8]), uint64(binary.LittleEndian.Uint32(header[8:]))
		if start < sequence || count > statsAggregateBatchOperations || start > attemptAdmissionMaxSequence || count > attemptAdmissionMaxSequence-start {
			return head, sequence, rows, errors.New("private aggregate admission WAL sequence or batch count is invalid")
		}
		var staged statsAggregateAdmissionHead
		stagedRows := false
		for index := uint64(0); index < count; index++ {
			if err := ctx.Err(); err != nil {
				return head, sequence, rows, err
			}
			kind, err := reader.ReadByte()
			if err != nil {
				return head, sequence, rows, err
			}
			keyLength, err := binary.ReadUvarint(reader)
			if err != nil {
				return head, sequence, rows, err
			}
			var keyBuffer [73]byte
			if keyLength == 0 || keyLength > uint64(len(keyBuffer)) {
				return head, sequence, rows, errors.New("private aggregate admission WAL key is oversized")
			}
			key := keyBuffer[:int(keyLength)]
			if _, err := io.ReadFull(reader, key); err != nil {
				return head, sequence, rows, err
			}
			limit, err := statsAggregateAdmissionValueLimit(key, kind, self.bounds)
			if err != nil {
				return head, sequence, rows, err
			}
			var valueLength uint64
			if kind == 1 {
				valueLength, err = binary.ReadUvarint(reader)
				if err != nil {
					return head, sequence, rows, err
				}
			}
			if bytes.Equal(key, []byte{'h'}) {
				if count != 1 || valueLength > limit {
					return head, sequence, rows, errors.New("private aggregate admission WAL header shape differs")
				}
				staged, err = statsAggregateReadAdmissionHead(reader, valueLength, start+index, initial, self.bounds)
				if err != nil {
					return head, sequence, rows, err
				}
			} else {
				if valueLength != limit {
					return head, sequence, rows, errors.New("private aggregate admission WAL row shape differs")
				}
				if err := attemptAdmissionSkip(reader, valueLength, scratch); err != nil {
					return head, sequence, rows, err
				}
				stagedRows = true
			}
		}
		if !staged.found && reader.offset > statsAggregateBatchBytes {
			return head, sequence, rows, errors.New("private aggregate admission WAL batch exceeds byte bound")
		}
		if _, err := reader.ReadByte(); err != io.EOF {
			return head, sequence, rows, errors.Join(errors.New("private aggregate admission WAL batch has extra records"), err)
		}
		if err := ctx.Err(); err != nil {
			return head, sequence, rows, err
		}
		head, err = statsAggregateNewestAdmissionHead(head, staged)
		if err != nil {
			return head, sequence, rows, err
		}
		sequence, rows = start+count, rows || stagedRows
	}
}
