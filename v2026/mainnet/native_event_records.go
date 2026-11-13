// The shared SCALE traversal retains phase and transaction position while
// validating every event, including events outside an observer's selection.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

// Field slices borrow the bounded raw bundle for this synchronous visit only.
// No callback owns I/O or executes under a state lock.
type nativeEventRecord struct {
	index          uint64
	phase          byte
	extrinsicIndex *uint32
	event          rootReceiptEvent
	fields         [][]byte
}

func walkNativeEventRecords(metadata *types.Metadata, raw []byte, bodyCount int, events map[[2]byte]rootReceiptEvent, maximumEvents uint64, visit func(nativeEventRecord) error) error {
	if len(raw) == 0 || len(raw) > rootBodyBytesLimit || bodyCount < 0 || bodyCount > rootBodyCountLimit {
		return errors.New("native event bundle or body count exceeds its bound")
	}
	reader := rootScaleReader{data: raw, metadata: metadata}
	count, err := reader.compact()
	if err != nil || count > maximumEvents || count > uint64(len(raw)) {
		return errors.New("native event count exceeds its bound")
	}
	for eventIndex := uint64(0); eventIndex < count; eventIndex++ {
		phase, err := reader.take(1)
		if err != nil || phase[0] > 2 {
			return errors.New("native event phase is absent or unknown")
		}
		record := nativeEventRecord{index: eventIndex, phase: phase[0]}
		if record.phase == 0 {
			encoded, err := reader.take(4)
			if err != nil {
				return err
			}
			index := binary.LittleEndian.Uint32(encoded)
			if uint64(index) >= uint64(bodyCount) {
				return errors.New("native event phase refers outside block body")
			}
			record.extrinsicIndex = &index
		}
		encodedId, err := reader.take(2)
		if err != nil {
			return err
		}
		event, exists := events[[2]byte{encodedId[0], encodedId[1]}]
		if !exists {
			return errors.New("native event is absent from execution metadata")
		}
		record.event = event
		record.fields = make([][]byte, 0, len(event.variant.Fields))
		for _, field := range event.variant.Fields {
			start := reader.offset
			if err := reader.skip(field.Type, 0); err != nil {
				return fmt.Errorf("native event %s: %w", event.name, err)
			}
			record.fields = append(record.fields, raw[start:reader.offset])
		}
		topics, err := reader.compact()
		if err != nil || topics > uint64((len(raw)-reader.offset)/32) {
			return errors.New("native event topics are truncated")
		}
		if _, err := reader.take(int(topics) * 32); err != nil {
			return err
		}
		if err := visit(record); err != nil {
			return err
		}
	}
	if reader.offset != len(raw) {
		return errors.New("native event storage has trailing bytes")
	}
	return nil
}
