//go:build !linux && !darwin

package clientauth

// New durable registration requires descriptor-relative private custody;
// legacy authentication remains available on platforms without this owner.

import "errors"

type registrationStore struct{}

func openRegistrationStore(string) (*registrationStore, error) {
	return nil, errors.New("durable client registration requires Unix custody support")
}

func openRegistrationStoreForOwner(string, *registrationStore) (*registrationStore, error) {
	return nil, errors.New("provider custody requires Unix directory ownership")
}

func (self *registrationStore) check() error {
	return errors.New("durable client registration custody is unavailable")
}

func (self *registrationStore) remove(string) error {
	return errors.New("durable client registration custody is unavailable")
}

func (self *registrationStore) read(string) ([]byte, error) {
	return nil, errors.New("durable client registration custody is unavailable")
}

func (self *registrationStore) write(string, []byte) error {
	return errors.New("durable client registration custody is unavailable")
}

// Unsupported platforms cannot inspect or create provider custody.
func (self *registrationStore) names(int) ([]string, error) {
	return nil, errors.New("durable client registration custody is unavailable")
}

func (self *registrationStore) close() error { return nil }
