//go:build linux

package nativefee

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"slices"
	"syscall"

	"golang.org/x/sys/unix"
)

// RetainOriginals synchronously streams the exact completed verifier inputs.
// A durable consumer must call this inside the same transaction as fee credit,
// persisting every chunk before returning from sink. Any error must roll back
// all settlement effects. No completion flag or caller-selected file is used.
// Digest order provides a consistent lock order for shared immutable SQL blobs.
func (self *Verified) RetainOriginals(ctx context.Context, sink func(string, Reference, io.Reader) error) error {
	if self == nil || sink == nil {
		return errors.New("original native fee custody has no owned result or sink")
	}
	if err := self.Check(ctx, self.authority); err != nil {
		return err
	}
	originals := slices.Clone(self.statement.Originals)
	slices.SortFunc(originals, func(a, b Original) int {
		if order := cmp.Compare(a.Reference.Sha256, b.Reference.Sha256); order != 0 {
			return order
		}
		return cmp.Compare(a.Kind, b.Kind)
	})
	inodes := map[[2]uint64]bool{}
	for _, original := range originals {
		if err := retainOriginal(ctx, original, inodes, sink); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func retainOriginal(ctx context.Context, original Original, inodes map[[2]uint64]bool, sink func(string, Reference, io.Reader) error) (resultErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	limit := originalLimit(original.Kind)
	if limit == 0 || original.Reference.Validate() != nil {
		return errors.New("invalid original native fee proof reference")
	}
	fd, err := unix.Open(original.Reference.Path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	source := os.NewFile(uintptr(fd), original.Reference.Path)
	defer func() { resultErr = errors.Join(resultErr, source.Close(), ctx.Err()) }()
	before, err := source.Stat()
	if err != nil {
		return err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || stat.Nlink != 1 || before.Size() <= 0 || before.Size() > limit {
		return errors.New("native fee original is not a private bounded regular file")
	}
	identity := [2]uint64{uint64(stat.Dev), stat.Ino}
	if inodes[identity] {
		return errors.New("native fee original proof roles alias one inode")
	}
	inodes[identity] = true
	reader := &originalReader{ctx: ctx, file: source, remaining: before.Size(), digest: sha256.New()}
	if err := sink(original.Kind, original.Reference, reader); err != nil {
		return err
	}
	if reader.remaining != 0 {
		return errors.New("native fee original sink did not retain the complete file")
	}
	var extra [1]byte
	if n, err := source.Read(extra[:]); n != 0 || err != io.EOF {
		return errors.New("native fee original changed while retaining")
	}
	after, err := source.Stat()
	if err != nil {
		return err
	}
	final, ok := after.Sys().(*syscall.Stat_t)
	if !ok || stat.Dev != final.Dev || stat.Ino != final.Ino || stat.Mode != final.Mode || stat.Nlink != final.Nlink || stat.Mtim != final.Mtim || stat.Ctim != final.Ctim || after.Size() != before.Size() || "sha256:"+hex.EncodeToString(reader.digest.Sum(nil)) != original.Reference.Sha256 {
		return errors.New("native fee retained original differs from verified input")
	}
	return nil
}

type originalReader struct {
	ctx       context.Context
	file      *os.File
	remaining int64
	digest    hash.Hash
}

func (self *originalReader) Read(raw []byte) (int, error) {
	if err := self.ctx.Err(); err != nil {
		return 0, err
	}
	if self.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(raw)) > self.remaining {
		raw = raw[:self.remaining]
	}
	n, err := self.file.Read(raw)
	self.remaining -= int64(n)
	if n > 0 {
		self.digest.Write(raw[:n])
	}
	if errors.Is(err, io.EOF) && self.remaining != 0 {
		err = io.ErrUnexpectedEOF
	}
	return n, errors.Join(err, self.ctx.Err())
}
