// Operational storage inputs remain outside signed protocol and journal bytes.
// Subcommands receive only an immutable reference and acquire their own owners.
package main

import (
	"context"
	"errors"
	"strings"

	"github.com/urnetwork/connect/v2026/durablevolume"
)

// Extract only the two shared options, preserving every other argument for its
// existing strict parser. Duplicate and partial declarations are never defaults.
func mainnetDurableVolumeArguments(ctx context.Context, args []string) (context.Context, []string, error) {
	if ctx == nil {
		return nil, nil, errors.New("mainnet command context is absent")
	}
	values := map[string]string{}
	remaining := make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		name, value, assigned := strings.Cut(args[index], "=")
		if name != "--durable-volumes" && name != "--durable-volumes-sha256" {
			remaining = append(remaining, args[index])
			continue
		}
		if _, duplicate := values[name]; duplicate {
			return nil, nil, errors.New("durable-volume option is repeated")
		}
		if !assigned {
			index++
			if index >= len(args) || strings.HasPrefix(args[index], "--") {
				return nil, nil, errors.New("durable-volume option requires a value")
			}
			value = args[index]
		}
		if value == "" {
			return nil, nil, errors.New("durable-volume option is empty")
		}
		values[name] = value
	}
	if len(values) == 0 {
		return ctx, remaining, nil
	}
	if len(values) != 2 {
		return nil, nil, errors.New("durable-volume declaration and exact hash are both required")
	}
	reference := durablevolume.Reference{Path: values["--durable-volumes"], Sha256: values["--durable-volumes-sha256"]}
	if retained, present := durablevolume.ReferenceFromContext(ctx); present && retained != reference {
		return nil, nil, errors.New("durable-volume command declaration differs from its caller")
	}
	return durablevolume.WithReference(ctx, reference), remaining, nil
}
