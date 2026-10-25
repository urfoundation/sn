// Close evidence is optional for providing. An explicit complete policy domain
// selects signing; a missing or invalid file leaves the ordinary close unsigned.
package miner

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Read once before the device owner starts; subsequent file changes cannot
// relabel originals already created by that owner. No network or signing occurs.
func readProviderCloseReportDomain(path string) ([32]byte, error) {
	if path == "" {
		return [32]byte{}, nil
	}
	file, err := openProviderCloseReportDomain(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return [32]byte{}, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > 16*1024 {
		return [32]byte{}, errors.New("close-report domain must be a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, 16*1024+1))
	if err != nil || len(data) > 16*1024 {
		return [32]byte{}, errors.Join(errors.New("close-report domain exceeds its fixed input bound"), err)
	}
	if err := protocol.ValidateUniqueJsonKeys(data); err != nil {
		return [32]byte{}, err
	}
	var domain protocol.ClientKeyHistoryDomain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&domain); err != nil {
		return [32]byte{}, err
	}
	return domain.Digest()
}
