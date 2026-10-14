package crv4

// Fleet capabilities compare only the interfaces consumed by one operation.
// Structural compatibility never approves an artifact or a signed transaction.
import (
	"fmt"
	"reflect"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

type FleetRuntimePurpose string

const (
	FleetRegistrationRead  FleetRuntimePurpose = "fleet-registration-read-v1"
	FleetRegistrationWrite FleetRuntimePurpose = "fleet-registration-write-v1"
	FleetCommitmentRead    FleetRuntimePurpose = "fleet-commitment-read-v1"
	FleetCommitmentWrite   FleetRuntimePurpose = "fleet-commitment-write-v1"
	FleetDispatchRead      FleetRuntimePurpose = "fleet-dispatch-read-v1"
	FleetFrontierRead      FleetRuntimePurpose = "fleet-frontier-read-v1"
	FleetFrontierWrite     FleetRuntimePurpose = "fleet-frontier-write-v1"
)

// This finite vocabulary prevents an unimplemented purpose from inheriting a
// general approval. Read profiles do not require unrelated signing extensions.
func KnownFleetRuntimePurpose(purpose FleetRuntimePurpose) bool {
	switch purpose {
	case FleetRegistrationRead, FleetRegistrationWrite, FleetCommitmentRead, FleetCommitmentWrite, FleetDispatchRead, FleetFrontierRead, FleetFrontierWrite:
		return true
	}
	return false
}

// Callers first authenticate exact approved code/metadata at the consumed
// block. This extra check detects unsupported consumed encoding changes.
func ValidateFleetRuntimeMetadata(metadata *types.Metadata, purpose FleetRuntimePurpose) error {
	storage, calls, events := map[string]string{}, map[string]string{}, map[string]string{}
	signing := false
	switch purpose {
	case FleetRegistrationRead, FleetRegistrationWrite:
		storage["System"] = "Account"
		storage["SubtensorModule"] = "Uids Owner Burn MinBurn MaxBurn BurnHalfLife BurnIncreaseMult"
		if purpose == FleetRegistrationWrite {
			calls["SubtensorModule"] = "register_limit"
			signing = true
		}
	case FleetCommitmentRead:
		storage["Commitments"] = "CommitmentOf LastCommitment"
	case FleetCommitmentWrite:
		storage["System"] = "Account"
		calls["Commitments"] = "set_commitment"
		signing = true
	case FleetDispatchRead:
		storage["System"] = "Events"
		events["System"] = "ExtrinsicSuccess ExtrinsicFailed"
	case FleetFrontierRead, FleetFrontierWrite:
		storage["Ethereum"] = "BlockHash"
	default:
		return fmt.Errorf("fleet runtime purpose %q is unimplemented", purpose)
	}
	baseline, err := runtimeProfileBaseline()
	if err != nil {
		return err
	}
	expected, err := runtimeProfileSelectedShape(baseline, storage, calls, events, signing)
	if err != nil {
		return err
	}
	actual, err := runtimeProfileSelectedShape(metadata, storage, calls, events, signing)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(expected))
	for name := range expected {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !reflect.DeepEqual(expected[name], actual[name]) {
			return fmt.Errorf("fleet runtime %s consumed interface %s changed", purpose, name)
		}
	}
	return nil
}
