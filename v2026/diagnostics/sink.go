// Strict admission applies only to long-lived diagnostics. Unknown synchronous
// Writers and ordinary files cannot promise cancellation and are never invoked.
package diagnostics

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
)

const maximumMemoryBytes = 1024 * 1024

// A caller-supplied context writer explicitly owns its cancellation contract.
// The exporter borrows it and never closes the embedding's original object.
type contextSink struct{ writer ContextWriter }

// The interface contract must interrupt the actual write and join its effects.
func (self *contextSink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	return self.writer.WriteContext(ctx, raw)
}

// Borrowed embedding ownership survives exporter shutdown.
func (self *contextSink) Close() error { return nil }

// In-memory embedding capture is finite too; no arbitrary callback is invoked.
// The caller may inspect its buffer after the exporter has joined.
type memorySink struct {
	buffer    *bytes.Buffer
	discarded bool
}

// A known memory operation cannot wait on an external device or another owner.
func (self *memorySink) WriteContext(ctx context.Context, raw []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if self.discarded {
		return len(raw), nil
	}
	if len(raw) > maximumMemoryBytes-self.buffer.Len() {
		return 0, errors.New("diagnostic memory capture is full")
	}
	return self.buffer.Write(raw)
}

// The embedding retains its finite buffer.
func (self *memorySink) Close() error { return nil }

// Only explicit context-aware or known finite adapters may reach the worker.
func adaptSink(writer io.Writer) (ownedSink, error) {
	value := reflect.ValueOf(writer)
	if !value.IsValid() {
		return nil, errors.New("diagnostic sink is absent")
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if value.IsNil() {
			return nil, errors.New("diagnostic sink is absent")
		}
	}
	if writer == io.Discard {
		return &memorySink{discarded: true}, nil
	}
	if value, ok := writer.(ContextWriter); ok {
		return &contextSink{writer: value}, nil
	}
	switch value := writer.(type) {
	case *bytes.Buffer:
		if value == nil {
			break
		}
		return &memorySink{buffer: value}, nil
	case *os.File:
		return newDescriptorSink(value)
	}
	return nil, errors.New("diagnostic sink has no admitted cancellation contract")
}
