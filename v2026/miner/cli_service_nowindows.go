//go:build !windows

package miner

import (
	"context"
	"errors"
)

func newWindowsService() (minerServiceManager, error) {
	return nil, errors.New("not Windows")
}

// minerServiceRun is the Windows supervisor's hidden command; elsewhere the
// service manager supervises provider start itself.
func minerServiceRun(args []string) (int, bool) { return 0, false }

func minerServiceStopContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return ctx, func() {}
}

func platformInstallerService(manager minerServiceManager) installerMiner { return nil }
