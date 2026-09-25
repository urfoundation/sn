// File authentication streams a fixed working set even for the all-in-one
// executable. Every invocation reads the current bytes; no path or metadata
// cache can authorize a changed executable or retained configuration.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

const fileHashBufferBytes = 64 * 1024

// Preserves the existing hash format and exact read errors for callers without
// an owner context. Startup uses the context-aware form to bound cancellation.
func fileSHA256(path string) (string, error) {
	return fileSHA256Context(context.Background(), path)
}

// Owns and closes only the descriptor opened for this authentication attempt.
func fileSHA256Context(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return streamSHA256Context(ctx, file)
}

// Borrows the reader. The wrapper deliberately exposes no WriterTo shortcut,
// so io.CopyBuffer cannot replace the fixed read bound with a whole-file read.
func streamSHA256Context(ctx context.Context, reader io.Reader) (string, error) {
	hash := sha256.New()
	bufferBytes := fileHashBufferBytes
	if file, ok := reader.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return "", err
		}
		// The size only bounds scratch space, never the bytes read. A file
		// growing after Stat is still consumed through EOF and never cached.
		if info.Mode().IsRegular() && info.Size() < int64(bufferBytes) {
			bufferBytes = max(1, int(info.Size()))
		}
	}
	buffer := make([]byte, bufferBytes)
	if _, err := io.CopyBuffer(hash, &fileHashReader{ctx: ctx, reader: reader}, buffer); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

// Cancellation is checked between bounded file reads, before hashing more data.
type fileHashReader struct {
	ctx    context.Context
	reader io.Reader
}

// Read only while this authentication owner remains active.
func (self *fileHashReader) Read(buffer []byte) (int, error) {
	if err := self.ctx.Err(); err != nil {
		return 0, err
	}
	return self.reader.Read(buffer)
}
