//go:build linux

package main

// These borrowed descriptor checks carry no namespace or provenance authority.
// Each fixed owner separately supplies its grammar, original scope and limits.
import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// Borrowed roots are observed through their descriptor. The outer preparation
// owner retains ancestry, complete target census and all publication barriers.
func storageOriginalRoot(ctx context.Context, root *os.File, expectedPath string) (unix.Stat_t, error) {
	var stat unix.Stat_t
	if ctx == nil || root == nil {
		return stat, errors.New("original custody inspection requires its borrowed root")
	}
	if err := ctx.Err(); err != nil {
		return stat, err
	}
	if err := unix.Fstat(int(root.Fd()), &stat); err != nil {
		return stat, mainnetDurableUnavailable("cannot inspect original custody root", err)
	}
	if root.Name() != expectedPath || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&07777 != 0700 || stat.Uid != uint32(os.Geteuid()) {
		return stat, errors.Join(durablevolume.ErrIdentity, errors.New("original custody root differs from its approved private logical path"))
	}
	return stat, nil
}

// A read owns only its temporary descriptor; its opened and named state must
// agree before and after bounded I/O. Failed observations precede comparisons.
func readStorageOriginalFile(ctx context.Context, root *os.File, parent unix.Stat_t, member durablevolume.PreparationFile, maximum uint64) (raw []byte, observed unix.Stat_t, resultErr error) {
	defer func() {
		if resultErr != nil {
			raw = nil
		}
	}()
	if ctx == nil || root == nil || maximum >= uint64(^uint(0)>>1) || member.Kind != "file" || member.Mode != 0400 && member.Mode != 0600 || member.Path == "" || member.Path == "." || member.Path == ".." || filepath.Base(member.Path) != member.Path || strings.ContainsRune(member.Path, 0) || member.Bytes > maximum {
		return nil, observed, errors.New("original custody inspection exceeds its fixed file profile")
	}
	if err := ctx.Err(); err != nil {
		return nil, observed, err
	}
	fd, err := unix.Openat(int(root.Fd()), member.Path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, observed, storageSdkWorkObservation("cannot open original custody original", err)
	}
	file := os.NewFile(uintptr(fd), member.Path)
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	if err := unix.Fstat(fd, &observed); err != nil {
		return nil, observed, mainnetDurableUnavailable("cannot observe original custody descriptor", err)
	}
	if observed.Dev != parent.Dev || observed.Mode&unix.S_IFMT != unix.S_IFREG || observed.Mode&07777 != member.Mode || observed.Uid != parent.Uid || observed.Nlink != 1 || observed.Size < 0 || uint64(observed.Size) != member.Bytes {
		return nil, observed, errors.Join(durablevolume.ErrIdentity, errors.New("original custody original has changed physical custody"))
	}
	raw = make([]byte, int(member.Bytes))
	for offset := 0; offset < len(raw); {
		if err := ctx.Err(); err != nil {
			return nil, observed, err
		}
		chunk := raw[offset:min(offset+64*1024, len(raw))]
		n, err := file.ReadAt(chunk, int64(offset))
		if err != nil {
			return nil, observed, mainnetDurableUnavailable("cannot read original custody original", err)
		}
		if n != len(chunk) {
			return nil, observed, mainnetDurableUnavailable("cannot completely read original custody original", io.ErrUnexpectedEOF)
		}
		offset += n
	}
	var after, named unix.Stat_t
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, observed, mainnetDurableUnavailable("cannot reobserve original custody descriptor", err)
	}
	if err := unix.Fstatat(int(root.Fd()), member.Path, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, observed, storageSdkWorkObservation("cannot reobserve original custody name", err)
	}
	if !bootstrapSuccessorMemberSameStat(observed, after) || !bootstrapSuccessorMemberSameStat(after, named) || safeReleaseHash(raw) != member.Sha256 {
		return nil, observed, errors.Join(durablevolume.ErrIdentity, errors.New("original custody original changed during inspection"))
	}
	return raw, observed, ctx.Err()
}
