//go:build !linux

package nativefee

import (
	"context"
	"errors"
	"io"
	"time"
)

func Invoke(context.Context, Authority, Reference, string, time.Duration) (*Verified, error) {
	return nil, errors.New("native fee verifier requires Linux sealed executable and owned process supervision")
}

func (*Verified) RetainOriginals(context.Context, func(string, Reference, io.Reader) error) error {
	return errors.New("native fee original custody requires Linux protected-file ownership")
}
