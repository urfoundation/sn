//go:build linux || darwin

// Production activation retains the original document and publishes its exact
// rendered replacement through the guarded descriptor, with both sync barriers.
package validator

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func rewriteReleaseConfigDurable(ctx context.Context, path string, original, rendered []byte, mode os.FileMode) (resultErr error) {
	directory, err := openAttemptPrivateDirectory(filepath.Dir(path), ctx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.close()) }()
	name := filepath.Base(path)
	file, err := directory.openFile(name, unix.O_RDONLY, 0)
	if err != nil {
		return err
	}
	state, statErr := statAttemptPrivateFile(file)
	raw, readErr := io.ReadAll(io.LimitReader(file, int64(len(original))+1))
	if err := errors.Join(statErr, readErr, file.Close(), ctx.Err()); err != nil {
		return err
	}
	if !state.regular() || state.links != 1 || !bytes.Equal(raw, original) {
		return errors.New("activation configuration changed before physical admission")
	}
	if bytes.Equal(original, rendered) {
		return directory.check()
	}
	backup := name + ".pre-activation"
	file, err = directory.openFile(backup, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, uint32(mode.Perm()))
	if errors.Is(err, os.ErrExist) {
		file, err = directory.openFile(backup, unix.O_RDONLY, 0)
		if err != nil {
			return err
		}
		info, statErr := statAttemptPrivateFile(file)
		retained, readErr := io.ReadAll(io.LimitReader(file, int64(len(original))+1))
		if err := errors.Join(statErr, readErr, file.Close(), ctx.Err()); err != nil {
			return err
		}
		if !info.regular() || info.links != 1 || !bytes.Equal(retained, original) {
			return errors.New("original pre-activation configuration differs from retained backup")
		}
	} else if err != nil {
		return err
	} else {
		n, writeErr := file.Write(original)
		if n != len(original) {
			writeErr = errors.Join(writeErr, io.ErrShortWrite)
		}
		if err := errors.Join(writeErr, file.Sync(), file.Close(), directory.file.Sync()); err != nil {
			return errors.Join(ErrDurablePublicationUncertain, err)
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	stage := ".activation-" + hex.EncodeToString(nonce[:])
	file, err = directory.openFile(stage, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, uint32(mode.Perm()))
	if err != nil {
		return err
	}
	fd := int(directory.file.Fd())
	defer unix.Unlinkat(fd, stage, 0)
	n, writeErr := file.Write(rendered)
	if n != len(rendered) {
		writeErr = errors.Join(writeErr, io.ErrShortWrite)
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close(), ctx.Err(), directory.check(), directory.storage.CheckWrite()); err != nil {
		return err
	}
	current, err := directory.stat(name)
	if err != nil || current != state {
		return errors.Join(errors.New("original configuration changed before publication"), err)
	}
	if err := unix.Renameat(fd, stage, fd, name); err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	if err := errors.Join(directory.file.Sync(), directory.check()); err != nil {
		return errors.Join(ErrDurablePublicationUncertain, err)
	}
	return nil
}
