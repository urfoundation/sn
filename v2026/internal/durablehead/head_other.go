//go:build !linux && !darwin

// Unsupported hosts cannot silently downgrade declared custody to pathname I/O.
package durablehead

import (
	"context"
	"os"

	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

type Owner struct{}

func Open(context.Context, *durablepath.Directory, *os.File, Spec) (*Owner, error) {
	return nil, durablevolume.ErrUnsupported
}
func OpenReadOnly(context.Context, *durablepath.Directory, *os.File, Spec) (*Owner, error) {
	return nil, durablevolume.ErrUnsupported
}
func Reconcile(context.Context, *durablepath.Directory, *os.File, Spec) (*Owner, error) {
	return nil, durablevolume.ErrUnsupported
}
func (*Owner) Check() error                                    { return durablevolume.ErrUnsupported }
func (*Owner) CheckWrite() error                               { return durablevolume.ErrUnsupported }
func (*Owner) Read() ([]byte, bool, error)                     { return nil, false, durablevolume.ErrUnsupported }
func (*Owner) Publish([]byte, func(*os.File) error) error      { return durablevolume.ErrUnsupported }
func (*Owner) PublishWithHooks([]byte, PublicationHooks) error { return durablevolume.ErrUnsupported }
func (*Owner) WithAuxiliary(string, bool, func(*os.File) error) error {
	return durablevolume.ErrUnsupported
}
func (*Owner) Capacity() (int64, int64, error) { return 0, 0, durablevolume.ErrUnsupported }
func (*Owner) Close() error                    { return nil }
