// Bootstrap plan/apply/resume wires the existing root custody and service
// owners. This first local phase completes without enabling network effects.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

// The explicit accepted hash and run directory precede every mutation. A
// signature import additionally pins exact public receipt bytes, never a key.
func runBootstrapRootCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[1] != "plan" && args[1] != "apply" && args[1] != "resume" {
		fmt.Fprintln(stderr, "bootstrap requires plan, apply or resume for the existing-root-custody phase")
		return 2
	}
	command := args[1]
	flags := flag.NewFlagSet("bootstrap "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "canonical private JSON bootstrap config")
	runDirectory := flags.String("run-dir", "", "exact precreated private run directory for apply/resume")
	accepted := flags.String("accept-plan-hash", "", "explicitly accepted local custody plan hash")
	signaturePath := flags.String("signature-file", "", "public native signature receipt for resume")
	signatureHash := flags.String("signature-sha256", "", "sha256 of exact public receipt bytes")
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || *configPath == "" ||
		command == "plan" && (*runDirectory != "" || *accepted != "") ||
		command != "plan" && (*runDirectory == "" || !planSha256(*accepted)) ||
		(*signaturePath == "") != (*signatureHash == "") || *signaturePath != "" && (command != "resume" || !planSha256(*signatureHash)) {
		fmt.Fprintln(stderr, "bootstrap plan needs --config; apply/resume also need --run-dir and --accept-plan-hash; only resume accepts an exact public signature pin")
		return 2
	}
	plan, err := loadBootstrapRootPlan(ctx, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "bootstrap plan:", err)
		return 2
	}
	encoder := json.NewEncoder(stdout)
	if command == "plan" {
		if err := encoder.Encode(plan); err != nil {
			fmt.Fprintln(stderr, "bootstrap plan output:", err)
			return 1
		}
		return 0
	}
	if *accepted != plan.ContentHash || *runDirectory != plan.RunDirectory {
		fmt.Fprintln(stderr, "bootstrap accepted plan or run directory differs; no journal opened")
		return 3
	}
	if plan.PassiveService != nil && *signaturePath != "" {
		fmt.Fprintln(stderr, "passive root preparation cannot import native signatures")
		return 3
	}
	var receipt *rootOfflineSignature
	if *signaturePath != "" {
		raw, digest, err := readBootstrapRootFile(ctx, *signaturePath, 16*1024)
		if err != nil || digest != *signatureHash {
			fmt.Fprintln(stderr, "bootstrap signature input:", errors.Join(errors.New("public signature content pin differs"), err))
			return 2
		}
		receipt = &rootOfflineSignature{}
		if err := decodePlanJson(raw, receipt); err != nil {
			fmt.Fprintln(stderr, "bootstrap signature input:", err)
			return 2
		}
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, "bootstrap canceled:", err)
		return 1
	}
	store, err := openBootstrapRootStore(plan, command == "apply", ctx)
	if err != nil {
		fmt.Fprintln(stderr, "bootstrap retained ownership:", err)
		return 3
	}
	defer store.close()
	owner, err := newBootstrapRootOwner(plan, store)
	if err != nil {
		fmt.Fprintln(stderr, "bootstrap retained progress:", err)
		return 3
	}
	result, err := owner.advance(ctx, receipt)
	if err != nil {
		fmt.Fprintln(stderr, "bootstrap local phase stopped; retain journals for resume:", err)
		return 1
	}
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(stderr, "bootstrap output failed; resume the retained journals:", err)
		return 1
	}
	return 0
}
