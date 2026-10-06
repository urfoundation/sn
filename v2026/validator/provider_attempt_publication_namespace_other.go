//go:build !linux

// Unsupported prepared physical custody remains explicit evidence unavailability.
package validator

import (
	"context"
	"errors"
	"os"
)

type ProviderAttemptPublicationNamespace struct{}

func FreshProviderAttemptPublicationNamespaceAttribute(ProviderAttemptPublicationPreparation, os.FileInfo) ([]byte, error) {
	return nil, errors.New("provider publication prepared custody is unavailable on this platform")
}
func OpenProviderAttemptPublicationNamespace(context.Context, ProviderAttemptPublicationPreparation) (*ProviderAttemptPublicationNamespace, error) {
	return nil, errors.New("provider publication prepared custody is unavailable on this platform")
}
func (*ProviderAttemptPublicationNamespace) Check(context.Context, *os.File) error {
	return errors.New("provider publication prepared custody is unavailable on this platform")
}
func (*ProviderAttemptPublicationNamespace) Close() error { return nil }
