// Stake-read compatibility is separate from transaction or production runtime
// authority. The caller first authenticates its exact artifact at one block;
// this capability only admits the storage and runtime API consumed below.
package crv4

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
)

const validatorStakeRuntimePurpose = "validator stake"
const validatorScheduleRuntimePurpose = "validator schedule"

// Metadata14 exposes storage shapes but not custom runtime API signatures.
// The API's declared version and complete bounded response decoder together
// preserve the reviewed selective-metagraph wire contract. Economic semantics
// remain the independently approved artifact's responsibility.
func validateValidatorReadRuntimeAtContext(ctx context.Context, chain *Chain, artifact AuthenticatedRuntimeArtifact, purpose string) error {
	if purpose != validatorStakeRuntimePurpose && purpose != validatorScheduleRuntimePurpose {
		return errors.New("validator runtime read purpose is unsupported")
	}
	version := artifact.Version
	if version.SpecName != "node-subtensor" || version.TransactionVersion != 1 || version.StateVersion != 1 {
		return errors.New("validator stake runtime family or encoding versions are unsupported")
	}
	if artifact.CompatibilityProfile != "" {
		if !chain.RuntimeArtifactCompatible(artifact) {
			return errors.New("validator stake runtime has no authenticated consumed-interface profile")
		}
		return ctx.Err()
	}
	// Preserve the exact historical adapters already reviewed for this layout.
	// New versions use the capability below without adding another spec entry.
	switch version.SpecVersion {
	case 454, 455, 458, 459, 460, 461, 467:
		return ctx.Err()
	}
	baseline, err := runtimeProfileBaseline()
	if err != nil {
		return err
	}
	storageNamesKVs := map[string]string{
		PalletName: "SubnetworkN Keys Uids Owner TotalHotkeyAlpha ValidatorPermit StakeThreshold SubnetOwnerHotkey",
	}
	if purpose == validatorScheduleRuntimePurpose {
		storageNamesKVs[PalletName] += " SubnetEpochIndex"
	}
	expectedKVs, err := runtimeProfileSelectedShape(baseline, storageNamesKVs, nil, nil, false)
	if err != nil {
		return fmt.Errorf("%s baseline capability: %w", purpose, err)
	}
	actualKVs, err := runtimeProfileSelectedShape(artifact.Metadata, storageNamesKVs, nil, nil, false)
	if err != nil {
		return fmt.Errorf("%s storage capability: %w", purpose, err)
	}
	// Stable ordering gives the same first unsupported capability regardless of
	// map iteration. Unrelated metadata changes do not participate.
	var names []string
	for name := range expectedKVs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if !reflect.DeepEqual(actualKVs[name], expectedKVs[name]) {
			return fmt.Errorf("%s consumed interface %s changed", purpose, name)
		}
	}
	var raw json.RawMessage
	if err := chain.API.Client.CallContext(ctx, &raw, "state_getRuntimeVersion", artifact.BlockHash.Hex()); err != nil {
		return fmt.Errorf("validator stake runtime API capability: %w", err)
	}
	observed, err := DecodeRuntimeVersionIdentity(raw)
	if err != nil || observed != artifact.Version {
		return errors.Join(errors.New("validator stake runtime changed during capability observation"), err)
	}
	if err := validateProvisionalRuntimeApis(raw); err != nil {
		return fmt.Errorf("validator stake selective-metagraph capability: %w", err)
	}
	return ctx.Err()
}
