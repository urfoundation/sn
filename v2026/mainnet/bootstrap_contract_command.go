// The executable contract phase prepares or imports public custody offline.
// Only explicit online resume can reconcile; only --submit permits one write.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
)

// Commands preserve original custody even when result publication fails.
// An explicit action selects one of the eight implemented graph reservations.
// The default and original reserve journal remain unchanged.
func runBootstrapContractCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "bootstrap-contracts" {
		fmt.Fprintln(stderr, "usage: bootstrap-contracts preview|plan|apply|resume --config FILE")
		return 2
	}
	command := args[1]
	if command != "preview" && command != "plan" && command != "apply" && command != "resume" {
		fmt.Fprintln(stderr, "unknown contract phase command")
		return 2
	}
	flags := flag.NewFlagSet("bootstrap-contracts "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "phase configuration; only preview accepts an unsigned draft")
	actionId := flags.String("action", "reserve-create", "implemented approved action: reserve-create, vault-create, coordinator-create, escrow-register, proxy-create, reserve-link, vault-link or evidence-create")
	accepted := flags.String("accept-plan-hash", "", "exact reviewed phase hash")
	runDirectory := flags.String("run-dir", "", "approved private journal directory")
	signaturePath := flags.String("signed-transaction", "", "private regular file containing original public transaction bytes")
	signatureHash := flags.String("signed-transaction-hash", "", "sha256 pin of original public signed-byte file")
	online := flags.Bool("online", false, "read the independently approved owned RPC route")
	submit := flags.Bool("submit", false, "permit one originally approved, durably counted HTTP submission")
	review := command == "plan" || command == "preview"
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || *configPath == "" || (*actionId != "reserve-create" && *actionId != "vault-create" && *actionId != "coordinator-create" && *actionId != "escrow-register" && *actionId != "proxy-create" && *actionId != "reserve-link" && *actionId != "vault-link" && *actionId != "evidence-create") || review && (*accepted != "" || *runDirectory != "" || *signaturePath != "" || *signatureHash != "" || *online || *submit) || !review && (*accepted == "" || *runDirectory == "") || (*signaturePath == "") != (*signatureHash == "") || *signaturePath != "" && (command != "resume" || !planSha256(*signatureHash)) || (*online || *submit) && command != "resume" || *submit && !*online {
		fmt.Fprintln(stderr, "contract phase requires exact config/plan/run directory; only resume accepts pinned signed bytes, --online and --submit")
		return 2
	}
	encoder := json.NewEncoder(stdout)
	if command == "preview" {
		preview, err := loadEvmPhasePreviewAction(ctx, *configPath, *actionId)
		if err != nil {
			fmt.Fprintln(stderr, "contract phase unsigned preview:", err)
			return 2
		}
		if err := encoder.Encode(preview); err != nil {
			fmt.Fprintln(stderr, "contract phase unsigned preview output:", err)
			return 1
		}
		return 0
	}
	plan, err := loadEvmCreatePlan(ctx, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "contract phase authority:", err)
		return 2
	}
	plan, err = selectEvmCreatePlan(ctx, plan, *actionId, *configPath)
	if err != nil {
		fmt.Fprintln(stderr, "contract phase selected action:", err)
		return 2
	}
	if command == "plan" {
		if err := encoder.Encode(struct {
			Plan                 evmPhasePlan                 `json:"plan"`
			PlanHash             string                       `json:"plan_hash"`
			ExecutableAction     string                       `json:"executable_action"`
			InstallationComplete bool                         `json:"installation_complete"`
			VaultConstructor     *contractVaultConstructor    `json:"vault_constructor,omitempty"`
			EscrowRegistration   *contractEscrowRegistration  `json:"escrow_registration,omitempty"`
			ProxyConstructor     *contractProxyConstructor    `json:"proxy_constructor,omitempty"`
			ReserveBinding       *contractReserveBinding      `json:"reserve_binding,omitempty"`
			VaultBinding         *contractVaultBinding        `json:"vault_binding,omitempty"`
			EvidenceConstructor  *contractEvidenceConstructor `json:"evidence_constructor,omitempty"`
		}{Plan: plan.Config.Plan, PlanHash: plan.Config.Plan.hash(), ExecutableAction: *actionId, InstallationComplete: false, VaultConstructor: plan.VaultConstructor, EscrowRegistration: plan.EscrowRegistration, ProxyConstructor: plan.ProxyConstructor, ReserveBinding: plan.ReserveBinding, VaultBinding: plan.VaultBinding, EvidenceConstructor: plan.EvidenceConstructor}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if *accepted != plan.Config.Plan.hash() || *runDirectory != plan.Config.Plan.RunDirectory {
		fmt.Fprintln(stderr, "contract phase acceptance differs; no journal opened")
		return 3
	}
	var signed []byte
	if *signaturePath != "" {
		for _, name := range []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile, evmProxyCreateStateFile, evmReserveLinkStateFile, evmVaultLinkStateFile, evmEvidenceCreateStateFile}[:plan.ActionIndex+1] {
			state := filepath.Join(*runDirectory, name)
			if *signaturePath == state || *signaturePath == state+".lock" {
				fmt.Fprintln(stderr, "signed-byte input aliases custody")
				return 2
			}
		}
		raw, digest, err := readBootstrapRootFile(ctx, *signaturePath, 128*1024)
		if err != nil || digest != *signatureHash {
			fmt.Fprintln(stderr, "signed-byte file:", errors.Join(errors.New("exact public signed-byte pin differs"), err))
			return 2
		}
		if _, err := plan.Config.Plan.Actions[plan.ActionIndex].signed(raw); err != nil {
			fmt.Fprintln(stderr, "signed-byte envelope:", err)
			return 2
		}
		signed = raw
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var store, reserveStore, vaultStore, coordinatorStore, escrowStore, proxyStore, reserveLinkStore, vaultLinkStore *evmActionStore
	if plan.ActionIndex > 0 {
		reserveStore, err = openEvmActionStore(plan.Config, false, nil, ctx)
		if err != nil {
			fmt.Fprintln(stderr, "contract phase reserve prerequisite custody:", err)
			return 3
		}
		defer reserveStore.close()
		reserve, loadErr := reserveStore.load()
		if loadErr != nil {
			fmt.Fprintln(stderr, "contract phase reserve prerequisite:", loadErr)
			return 3
		}
		if plan.ActionIndex == 1 {
			store, err = openEvmVaultActionStore(plan, reserve, command == "apply", nil, ctx)
		} else {
			vaultStore, err = openEvmVaultActionStore(*plan.Vault, reserve, false, nil, ctx)
			if err != nil {
				fmt.Fprintln(stderr, "contract phase vault prerequisite custody:", err)
				return 3
			}
			defer vaultStore.close()
			vault, loadErr := vaultStore.load()
			if loadErr != nil {
				fmt.Fprintln(stderr, "contract phase vault prerequisite:", loadErr)
				return 3
			}
			if plan.ActionIndex == 2 {
				store, err = openEvmCoordinatorActionStore(plan, reserve, vault, command == "apply", nil, ctx)
			} else {
				coordinatorStore, err = openEvmCoordinatorActionStore(*plan.Coordinator, reserve, vault, false, nil, ctx)
				if err != nil {
					fmt.Fprintln(stderr, "contract phase coordinator prerequisite custody:", err)
					return 3
				}
				defer coordinatorStore.close()
				coordinator, loadErr := coordinatorStore.load()
				if loadErr != nil {
					fmt.Fprintln(stderr, "contract phase coordinator prerequisite:", loadErr)
					return 3
				}
				if plan.ActionIndex == 3 {
					store, err = openEvmEscrowActionStore(plan, reserve, vault, coordinator, command == "apply", nil, ctx)
				} else {
					escrowStore, err = openEvmEscrowActionStore(*plan.Escrow, reserve, vault, coordinator, false, nil, ctx)
					if err != nil {
						fmt.Fprintln(stderr, "contract phase escrow prerequisite custody:", err)
						return 3
					}
					defer escrowStore.close()
					escrow, loadErr := escrowStore.load()
					if loadErr != nil {
						fmt.Fprintln(stderr, "contract phase escrow prerequisite:", loadErr)
						return 3
					}
					if plan.ActionIndex == 4 {
						store, err = openEvmProxyActionStore(plan, reserve, vault, coordinator, escrow, command == "apply", nil, ctx)
					} else {
						proxyStore, err = openEvmProxyActionStore(*plan.Proxy, reserve, vault, coordinator, escrow, false, nil, ctx)
						if err != nil {
							fmt.Fprintln(stderr, "contract phase proxy prerequisite custody:", err)
							return 3
						}
						defer proxyStore.close()
						proxy, loadErr := proxyStore.load()
						if loadErr != nil {
							fmt.Fprintln(stderr, "contract phase proxy prerequisite:", loadErr)
							return 3
						}
						if plan.ActionIndex == 5 {
							store, err = openEvmReserveLinkActionStore(plan, reserve, vault, coordinator, escrow, proxy, command == "apply", nil, ctx)
						} else {
							reserveLinkStore, err = openEvmReserveLinkActionStore(*plan.ReserveLink, reserve, vault, coordinator, escrow, proxy, false, nil, ctx)
							if err != nil {
								fmt.Fprintln(stderr, "contract phase reserve binding prerequisite custody:", err)
								return 3
							}
							defer reserveLinkStore.close()
							link, loadErr := reserveLinkStore.load()
							if loadErr != nil {
								fmt.Fprintln(stderr, "contract phase reserve binding prerequisite:", loadErr)
								return 3
							}
							if plan.ActionIndex == 6 {
								store, err = openEvmVaultLinkActionStore(plan, reserve, vault, coordinator, escrow, proxy, link, command == "apply", nil, ctx)
							} else {
								vaultLinkStore, err = openEvmVaultLinkActionStore(*plan.VaultLink, reserve, vault, coordinator, escrow, proxy, link, false, nil, ctx)
								if err != nil {
									fmt.Fprintln(stderr, "contract phase vault binding prerequisite custody:", err)
									return 3
								}
								defer vaultLinkStore.close()
								bound, loadErr := vaultLinkStore.load()
								if loadErr != nil {
									fmt.Fprintln(stderr, "contract phase vault binding prerequisite:", loadErr)
									return 3
								}
								store, err = openEvmEvidenceActionStore(plan, reserve, vault, coordinator, escrow, proxy, link, bound, command == "apply", nil, ctx)
							}
						}
					}
				}
			}
		}
	} else {
		store, err = openEvmActionStore(plan.Config, command == "apply", nil, ctx)
	}
	if err != nil {
		fmt.Fprintln(stderr, "contract phase retained ownership:", err)
		return 3
	}
	defer store.close()
	var chain evmActionChain
	if *online {
		adapter, err := newEvmOwnedChain(plan.Config)
		if err != nil {
			fmt.Fprintln(stderr, "contract phase owned route:", err)
			return 3
		}
		chain = adapter
		defer adapter.client.httpClient.CloseIdleConnections()
	}
	var owner *evmCreateOwner
	if plan.ActionIndex == 7 {
		owner, err = newEvmEvidenceCreateOwner(plan, store, reserveStore, vaultStore, coordinatorStore, escrowStore, proxyStore, reserveLinkStore, vaultLinkStore, chain)
	} else if plan.ActionIndex == 6 {
		owner, err = newEvmVaultLinkOwner(plan, store, reserveStore, vaultStore, coordinatorStore, escrowStore, proxyStore, reserveLinkStore, chain)
	} else if plan.ActionIndex == 5 {
		owner, err = newEvmReserveLinkOwner(plan, store, reserveStore, vaultStore, coordinatorStore, escrowStore, proxyStore, chain)
	} else if plan.ActionIndex == 4 {
		owner, err = newEvmProxyCreateOwner(plan, store, reserveStore, vaultStore, coordinatorStore, escrowStore, chain)
	} else if plan.ActionIndex == 3 {
		owner, err = newEvmEscrowRegisterOwner(plan, store, reserveStore, vaultStore, coordinatorStore, chain)
	} else if plan.ActionIndex == 2 {
		owner, err = newEvmCoordinatorCreateOwner(plan, store, reserveStore, vaultStore, chain)
	} else if plan.ActionIndex == 1 {
		owner, err = newEvmVaultCreateOwner(plan, store, reserveStore, chain)
	} else {
		owner, err = newEvmCreateOwner(plan, store, chain)
	}
	if err != nil {
		fmt.Fprintln(stderr, "contract phase retained state:", err)
		return 3
	}
	result, err := owner.advance(ctx, signed, *online, *submit)
	if err != nil {
		fmt.Fprintln(stderr, "contract phase stopped; retain original journal for resume:", err)
		return 1
	}
	if err := encoder.Encode(result); err != nil {
		fmt.Fprintln(stderr, "contract phase output failed; resume authoritative journal:", err)
		return 1
	}
	return 0
}
