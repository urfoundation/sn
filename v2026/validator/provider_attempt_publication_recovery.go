//go:build linux || darwin

// Reconcile only the exact final/temporary pair left by this namespace's
// no-replace publisher. Unrelated temporary evidence remains bounded and inert.
package validator

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"golang.org/x/sys/unix"
)

// The caller holds the namespace publication lease and has already verified
// the complete signed window and exact bytes at this original final pathname.
func reconcileProviderPublicationLinks(ctx context.Context, owner *releaseMeasurementInputV2Owner, raw []byte, maximumFiles uint64) (resultErr error) {
	if owner == nil || owner.witness == nil {
		return durablevolume.ErrIdentity
	}
	prior := *owner.witness
	if prior.links == 1 {
		return nil
	}
	if prior.links != 2 {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider publication has unrelated physical links"))
	}
	directory, err := openAttemptPrivateDirectory(owner.directory.path, owner.directory.storageCtx)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.check(), directory.close(), ctx.Err()) }()
	if directory.anchor.dev != owner.directory.anchor.dev || directory.anchor.ino != owner.directory.anchor.ino {
		return durablevolume.ErrIdentity
	}
	temporary := ""
	var count uint64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := directory.file.ReadDir(1)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return readErr
		}
		for _, entry := range entries {
			if count >= maximumFiles {
				return errors.Join(durablevolume.ErrIdentity, errors.New("provider publication link census exceeds its original bound"))
			}
			count++
			name := entry.Name()
			suffix, ok := strings.CutPrefix(name, ".compact-input-")
			if !ok || len(suffix) != 32 {
				continue
			}
			decoded, err := hex.DecodeString(suffix)
			if err != nil || hex.EncodeToString(decoded) != suffix {
				continue
			}
			state, err := directory.stat(name)
			if err != nil {
				return err
			}
			if state.dev == prior.dev && state.ino == prior.ino {
				if state != prior || temporary != "" {
					return durablevolume.ErrIdentity
				}
				temporary = name
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if temporary == "" {
		return errors.Join(durablevolume.ErrIdentity, errors.New("provider publication extra link is not its original temporary"))
	}
	file, err := directory.openFile(temporary, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	before, statErr := statAttemptPrivateFile(file)
	if statErr != nil || before != prior {
		return errors.Join(durablevolume.ErrIdentity, statErr, file.Close())
	}
	observed, readErr := io.ReadAll(io.LimitReader(file, int64(len(raw))+1))
	after, statErr := statAttemptPrivateFile(file)
	closeErr := file.Close()
	if err := errors.Join(readErr, statErr, closeErr); err != nil {
		return err
	}
	if after != prior || !bytes.Equal(observed, raw) {
		return durablevolume.ErrIdentity
	}
	if err := owner.check(); err != nil {
		return err
	}
	named, err := directory.stat(temporary)
	if err != nil {
		return err
	}
	if named != prior {
		return durablevolume.ErrIdentity
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.Unlinkat(int(directory.file.Fd()), temporary, 0); err != nil {
		return err
	}
	final, err := directory.stat(owner.name)
	if err != nil {
		return err
	}
	if final.links != 1 || final.dev != prior.dev || final.ino != prior.ino || final.mode != prior.mode || final.uid != prior.uid || final.size != prior.size || final.modifySeconds != prior.modifySeconds || final.modifyNanoseconds != prior.modifyNanoseconds {
		return durablevolume.ErrIdentity
	}
	owner.witness = &final
	return errors.Join(directory.file.Sync(), owner.check(), ctx.Err())
}
