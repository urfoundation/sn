//go:build linux || darwin

package clientauth

import (
	"bytes"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// Spends a completed provisioning with the store's publication discipline:
// one rename inside the owned directory descriptor, keeping the marker's bytes,
// checked before and after, then a directory sync. A directory without the
// marker has nothing to spend. A provisioned marker already in place lets only
// a leftover provisioning marker for the same key go.
func spendMeasurementProvisioning(store *registrationStore, expected []byte) error {
	marker, err := store.read(measurementProvisioningMarker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(marker, expected) {
		return errors.New("measurement provisioning marker differs from its retained public identity")
	}
	provisioned, err := store.read(measurementProvisionedMarker)
	if err == nil {
		if !bytes.Equal(provisioned, expected) {
			return errors.New("measurement provisioned marker differs from its retained public identity")
		}
		return store.remove(measurementProvisioningMarker)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := store.check(); err != nil {
		return err
	}
	directory := int(store.directory.Fd())
	if err := unix.Renameat(directory, measurementProvisioningMarker, directory, measurementProvisionedMarker); err != nil {
		return err
	}
	return errors.Join(store.directory.Sync(), store.check())
}
