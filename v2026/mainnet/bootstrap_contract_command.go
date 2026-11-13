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
// An explicit action selects one of the eight implemented graph reservations,
// or one of the settlement v2 actions of its separately approved graph.
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
	actionId := flags.String("action", "reserve-create", "implemented approved action: reserve-create, vault-create, coordinator-create, escrow-register, proxy-create, reserve-link, vault-link or evidence-create; settlement v2: vault2-create, escrow2-register, settlement2-create, proxy2-create, vault2-link or the optional batcher-create")
	accepted := flags.String("accept-plan-hash", "", "exact reviewed phase hash")
	runDirectory := flags.String("run-dir", "", "approved private journal directory")
	signaturePath := flags.String("signed-transaction", "", "private regular file containing original public transaction bytes")
	signatureHash := flags.String("signed-transaction-hash", "", "sha256 pin of original public signed-byte file")
	escrowPath := flags.String("escrow-signed-transaction", "", "settlement v2 vault2-create only: the escrow registration's original signed bytes, sent right after vault2's")
	escrowHash := flags.String("escrow-signed-transaction-hash", "", "sha256 pin of the escrow registration's signed-byte file")
	online := flags.Bool("online", false, "read the independently approved owned RPC route")
	submit := flags.Bool("submit", false, "permit one originally approved, durably counted HTTP submission")
	review := command == "plan" || command == "preview"
	if err := flags.Parse(args[2:]); err != nil || flags.NArg() != 0 || *configPath == "" || (*actionId != "reserve-create" && *actionId != "vault-create" && *actionId != "coordinator-create" && *actionId != "escrow-register" && *actionId != "proxy-create" && *actionId != "reserve-link" && *actionId != "vault-link" && *actionId != "evidence-create" && evmV2ActionIndex(*actionId) < 0) || review && (*accepted != "" || *runDirectory != "" || *signaturePath != "" || *signatureHash != "" || *online || *submit) || !review && (*accepted == "" || *runDirectory == "") || (*signaturePath == "") != (*signatureHash == "") || *signaturePath != "" && (command != "resume" || !planSha256(*signatureHash)) || (*escrowPath == "") != (*escrowHash == "") || *escrowPath != "" && (command != "resume" || *actionId != "vault2-create" || !planSha256(*escrowHash)) || (*online || *submit) && command != "resume" || *submit && !*online {
		fmt.Fprintln(stderr, "contract phase requires exact config/plan/run directory; only resume accepts pinned signed bytes, --online and --submit")
		return 2
	}
	encoder := json.NewEncoder(stdout)
	if command == "preview" {
		var preview any
		var err error
		if evmV2ActionIndex(*actionId) >= 0 {
			preview, err = loadEvmV2PhasePreview(ctx, *configPath, *actionId)
		} else {
			preview, err = loadEvmPhasePreviewAction(ctx, *configPath, *actionId)
		}
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
			Plan                   evmPhasePlan                    `json:"plan"`
			PlanHash               string                          `json:"plan_hash"`
			ExecutableAction       string                          `json:"executable_action"`
			InstallationComplete   bool                            `json:"installation_complete"`
			VaultConstructor       *contractVaultConstructor       `json:"vault_constructor,omitempty"`
			EscrowRegistration     *contractEscrowRegistration     `json:"escrow_registration,omitempty"`
			ProxyConstructor       *contractProxyConstructor       `json:"proxy_constructor,omitempty"`
			ReserveBinding         *contractReserveBinding         `json:"reserve_binding,omitempty"`
			VaultBinding           *contractVaultBinding           `json:"vault_binding,omitempty"`
			EvidenceConstructor    *contractEvidenceConstructor    `json:"evidence_constructor,omitempty"`
			Vault2Constructor      *contractVault2Constructor      `json:"vault2_constructor,omitempty"`
			Settlement2Initializer *contractSettlement2Initializer `json:"settlement2_initializer,omitempty"`
		}{Plan: plan.Config.Plan, PlanHash: plan.Config.Plan.hash(), ExecutableAction: *actionId, InstallationComplete: false, VaultConstructor: plan.VaultConstructor, EscrowRegistration: plan.EscrowRegistration, ProxyConstructor: plan.ProxyConstructor, ReserveBinding: plan.ReserveBinding, VaultBinding: plan.VaultBinding, EvidenceConstructor: plan.EvidenceConstructor, Vault2Constructor: plan.Vault2Constructor, Settlement2Initializer: plan.Settlement2Initializer}); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if *accepted != plan.Config.Plan.hash() || *runDirectory != plan.Config.Plan.RunDirectory {
		fmt.Fprintln(stderr, "contract phase acceptance differs; no journal opened")
		return 3
	}
	names := []string{evmCreateStateFile, evmVaultCreateStateFile, evmCoordinatorCreateStateFile, evmEscrowRegisterStateFile, evmProxyCreateStateFile, evmReserveLinkStateFile, evmVaultLinkStateFile, evmEvidenceCreateStateFile}
	if plan.Config.Plan.settlementV2() {
		names = evmV2JournalNames()
	}
	// Pinned public bytes of one approved envelope, never read from custody.
	readSigned := func(path, hash string, index int) ([]byte, bool) {
		for _, name := range names[:plan.ActionIndex+1] {
			state := filepath.Join(*runDirectory, name)
			if path == state || path == state+".lock" {
				fmt.Fprintln(stderr, "signed-byte input aliases custody")
				return nil, false
			}
		}
		raw, digest, err := readBootstrapRootFile(ctx, path, 128*1024)
		if err != nil || digest != hash {
			fmt.Fprintln(stderr, "signed-byte file:", errors.Join(errors.New("exact public signed-byte pin differs"), err))
			return nil, false
		}
		if _, err := plan.Config.Plan.Actions[index].signed(raw); err != nil {
			fmt.Fprintln(stderr, "signed-byte envelope:", err)
			return nil, false
		}
		return raw, true
	}
	var signed, escrow []byte
	if *signaturePath != "" {
		var ok bool
		if signed, ok = readSigned(*signaturePath, *signatureHash, plan.ActionIndex); !ok {
			return 2
		}
	}
	if *escrowPath != "" {
		var ok bool
		if escrow, ok = readSigned(*escrowPath, *escrowHash, 1); !ok {
			return 2
		}
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var store, reserveStore, vaultStore, coordinatorStore, escrowStore, proxyStore, reserveLinkStore, vaultLinkStore *evmActionStore
	var priorStores []evmActionStorage
	if plan.Config.Plan.settlementV2() {
		var stores []*evmActionStore
		stores, err = openEvmV2ActionStores(ctx, plan, command == "apply")
		for _, opened := range stores {
			defer opened.close()
		}
		if err == nil {
			store = stores[plan.ActionIndex]
			for _, prior := range stores[:plan.ActionIndex] {
				priorStores = append(priorStores, prior)
			}
		}
	} else if plan.ActionIndex > 0 {
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
	if plan.Config.Plan.settlementV2() {
		owner, err = newEvmSelectedCreateOwner(plan, store, priorStores, chain)
	} else if plan.ActionIndex == 7 {
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
	result, err := owner.advanceWithEscrow(ctx, signed, escrow, *online, *submit)
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
