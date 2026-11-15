// New service approvals bind storage policy independently of signed protocol
// inputs. Legacy omitted fields retain byte-identical signatures and argv.
package main

import (
	"context"
	"errors"
	"github.com/urfoundation/sn/v2026/internal/durablepath"
	"github.com/urnetwork/connect/v2026/durablevolume"
)

func validateUnitDurableReference(reference *durablevolume.Reference) error {
	if reference == nil {
		return nil
	}
	if !repairValidatorPath(reference.Path) || !planSha256(reference.Sha256) {
		return errors.New("service durable-volume reference is incomplete")
	}
	return nil
}

func unitDurableArguments(reference *durablevolume.Reference) string {
	if reference == nil {
		return ""
	}
	return " --durable-volumes=" + reference.Path + " --durable-volumes-sha256=" + reference.Sha256
}

// Actual production effects require the separately reviewed unit declaration;
// historical status/reconciliation still reads original omitted-field approval.
func requireUnitDurableReference(ctx context.Context, reference *durablevolume.Reference) error {
	if err := validateUnitDurableReference(reference); err != nil {
		return err
	}
	if err := durablepath.Require(ctx); err != nil {
		return err
	}
	if reference == nil {
		return errors.New("new service effects require an independently approved durable-volume reference")
	}
	selected, _ := durablevolume.ReferenceFromContext(ctx)
	if selected != *reference {
		return errors.New("service durable-volume reference differs from the admitted controller declaration")
	}
	_, err := durablevolume.Load(*reference)
	return err
}
