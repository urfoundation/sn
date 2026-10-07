//go:build !linux && !darwin

package clientauth

import "errors"

// Unsupported platforms cannot open the custody a provisioning would spend.
func spendMeasurementProvisioning(*registrationStore, []byte) error {
	return errors.New("durable client registration custody is unavailable")
}
