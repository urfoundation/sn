// An operational storage reference adds physical volume custody without
// rewriting signed validator configuration or retained execution transcripts.
package validator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Root installation can inspect the exact configured directories before writing
// the runtime file. Decoding is pure: no key, journal, RPC, or activation opens.
func ReleaseDurableDirectories(configPath string, raw []byte) ([]string, error) {
	config, err := decodeReleaseConfigDocument(configPath, raw)
	if err != nil {
		return nil, err
	}
	paths := []string{config.StateDir}
	add := func(path string) {
		if path != "" && !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	for _, operator := range config.Operators {
		add(operator.StateDir)
	}
	for _, operator := range config.EvidenceV2.Operators {
		add(operator.ReplayScratchRoot)
		add(operator.SealScratchRoot)
		for _, reference := range []ReleaseEvidenceV2File{operator.Activation, operator.VPKSignature, operator.HotkeySignature, operator.Context, operator.History} {
			if reference.Path != "" {
				add(filepath.Dir(reference.Path))
			}
		}
	}
	return paths, nil
}

// Publication uncertainty is separate from proven identity loss. A fresh
// owner must reconcile the exact retained bytes before dependent effects.
var ErrDurablePublicationUncertain = errors.New("validator durable publication is uncertain; reopen original custody")

func requireAttemptDurableReference(ctx context.Context, identity AttemptLedgerIdentity) error {
	if identity.ChainID == 964 {
		return durablepath.Require(ctx)
	}
	return nil
}

// Internal historical readers without a declaration retain their byte grammar.
// The production lifecycle admits an explicit declaration before any owner.
func validatorStorageContext(contexts []context.Context) context.Context {
	if len(contexts) == 0 {
		return context.Background()
	}
	if len(contexts) == 1 {
		return contexts[0]
	}
	return nil
}

func openValidatorDurableDirectory(ctx context.Context, path string, access durablevolume.Access, create bool) (*durablepath.Directory, error) {
	if ctx == nil {
		return nil, errors.New("validator storage context is absent")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, present := durablevolume.ReferenceFromContext(ctx); !present {
		return nil, nil
	}
	return durablepath.Open(ctx, path, access, create)
}

// This comparison joins the shared volume identity to the actual descriptor
// used by the original journal implementation, never a pathname-only preflight.
func checkValidatorDurableDirectory(storage *durablepath.Directory, file *os.File) error {
	return checkValidatorDurableDirectoryWithStat(storage, file, nil)
}

// The instance seam injects failed observations, never a validation verdict.
func checkValidatorDurableDirectoryWithStat(storage *durablepath.Directory, file *os.File, observe func(*os.File) (os.FileInfo, error)) error {
	if storage == nil {
		return nil
	}
	if err := storage.CheckRead(); err != nil {
		return err
	}
	if file == nil {
		return errors.New("validator durable directory is closed")
	}
	if observe == nil {
		observe = (*os.File).Stat
	}
	opened, err := observe(file)
	guarded, guardErr := observe(storage.File())
	if err != nil || guardErr != nil {
		cause := errors.Join(err, guardErr)
		if errors.Is(cause, os.ErrClosed) {
			return errors.Join(errors.New("validator directory descriptor is closed"), cause)
		}
		return errors.Join(&durablevolume.UnavailableError{Reason: "cannot observe validator directory descriptors"}, cause)
	}
	if !os.SameFile(opened, guarded) {
		return errors.Join(durablevolume.ErrIdentity, errors.New("validator directory differs from durable owner"))
	}
	return nil
}

// Runtime leases span the complete joined role lifetime. Individual I/O owners
// still open/check their own guard, so they also work outside this composition.
func retainReleaseDurableDirectories(ctx context.Context, cfg *ReleaseConfig) (func() error, error) {
	owners := []*durablepath.Directory{}
	closeAll := func() error {
		var result error
		for i := len(owners) - 1; i >= 0; i-- {
			result = errors.Join(result, owners[i].Close())
		}
		owners = nil
		return result
	}
	if err := requireReleaseDurableReference(ctx, cfg); err != nil {
		return nil, err
	}
	if _, present := durablevolume.ReferenceFromContext(ctx); !present {
		return closeAll, nil
	}
	paths := []string{cfg.StateDir}
	for _, operator := range cfg.Operators {
		paths = append(paths, operator.StateDir)
	}
	for _, path := range paths {
		owner, err := openValidatorDurableDirectory(ctx, path, durablevolume.ReadWrite, false)
		if err != nil {
			return nil, errors.Join(err, closeAll())
		}
		owners = append(owners, owner)
	}
	return closeAll, nil
}

// All public mainnet lifecycle variants require explicit custody before
// loading retained setup or creating any runtime owner. Legacy bytes stay intact.
func requireReleaseDurableReference(ctx context.Context, cfg *ReleaseConfig) error {
	if isOwnerRecycleProductionConfig(cfg) {
		return durablepath.Require(ctx)
	}
	return nil
}

// A derived projection may outlive the bounded startup call that created it.
// Preserve its immutable reference while each operation owns cancellation.
func validatorProofStorageContext(store *ProofStore) context.Context {
	if store.storageCtx != nil {
		return store.storageCtx
	}
	return context.Background()
}
