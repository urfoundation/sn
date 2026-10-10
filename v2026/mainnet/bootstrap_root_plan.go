// The first executable bootstrap phase binds an independently approved existing
// root action to local custody. It cannot authorize native issuance or a send.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const bootstrapRootConfigSchema = "urnetwork-mainnet-bootstrap-root-config-v1"
const bootstrapRootPlanSchema = "urnetwork-mainnet-bootstrap-root-custody-plan-v1"
const bootstrapRootPhase = "prepare-existing-root-custody"
const bootstrapRootProgressFile = "bootstrap-root.json"

// Network and service configuration are provisioned independently of retained
// progress. Relative paths, inherited routes and key flags are not accepted.
type bootstrapRootConfig struct {
	Schema       string            `json:"schema"`
	DeploymentId string            `json:"deployment_id"`
	Network      planNetwork       `json:"network"`
	RunDirectory string            `json:"run_directory"`
	RootService  planFileReference `json:"root_service"`
}

// The whole signed packet is reviewable before local mutation. Its native
// nonce, era and spend limits remain owned by the existing service/custody pair.
type bootstrapRootPlan struct {
	Schema         string                    `json:"schema"`
	Phase          string                    `json:"phase"`
	DeploymentId   string                    `json:"deployment_id"`
	Network        planNetwork               `json:"network"`
	RunDirectory   string                    `json:"run_directory"`
	ConfigPath     string                    `json:"config_path"`
	ConfigSha256   string                    `json:"config_sha256"`
	ServiceInput   planFileReference         `json:"service_input"`
	Service        rootServiceConfig         `json:"service"`
	PassiveService *rootPassiveServiceConfig `json:"passive_service,omitempty"`
	NetworkEffects bool                      `json:"network_effects"`
	NativeSigning  bool                      `json:"native_signing"`
	ContentHash    string                    `json:"content_hash"`
}

// The local phase has its own domain; a blocked review graph's hash is never
// interchangeable with this accepted plan, nor is this hash chain authority.
func bootstrapRootPlanHash(plan bootstrapRootPlan) string {
	plan.ContentHash = ""
	raw, _ := json.Marshal(plan)
	digest := sha256.Sum256(append([]byte(plan.Schema+"\x00"), raw...))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// Public action vectors are copied before the plan becomes an owner's input.
func copyBootstrapRootPlan(plan bootstrapRootPlan) bootstrapRootPlan {
	plan.Service = copyRootServiceConfig(plan.Service)
	if plan.PassiveService != nil {
		copy := *plan.PassiveService
		copy.Policy = copyRootPassivePolicy(copy.Policy)
		plan.PassiveService = &copy
	}
	return plan
}

// Every mutable destination is a distinct direct child of the approved private
// run directory. Input files cannot alias journals or their ownership markers.
func (self bootstrapRootPlan) validate() error {
	if self.Schema == bootstrapRootPassivePlanSchema {
		return self.validatePassive()
	}
	if self.PassiveService != nil {
		return errors.New("legacy root custody cannot acquire passive service authority")
	}
	if self.Schema != bootstrapRootPlanSchema || self.Phase != bootstrapRootPhase || !planLabel(self.DeploymentId) ||
		self.NetworkEffects || self.NativeSigning || !planSha256(self.ConfigSha256) || !planSha256(self.ServiceInput.Sha256) ||
		!bootstrapRootAbsolutePath(self.RunDirectory) || self.ContentHash != bootstrapRootPlanHash(self) {
		return errors.New("bootstrap root plan schema, scope or seal differs")
	}
	if err := self.Service.validate(); err != nil {
		return err
	}
	scope := self.Service.Packet.Action.Scope
	if self.Network.NativeChain != scope.NativeChain || self.Network.GenesisHash != scope.GenesisHash || self.Network.EvmChainId != mainnetEvmChainId || scope.EvmChainId != mainnetEvmChainId {
		return errors.New("bootstrap root plan differs from independently provisioned mainnet identity")
	}
	destinations := []string{filepath.Join(self.RunDirectory, bootstrapRootProgressFile), self.Service.CustodyTrust.StatePath, scope.StatePath}
	seen := map[string]bool{}
	for _, path := range destinations {
		if !bootstrapRootAbsolutePath(path) || filepath.Dir(path) != self.RunDirectory || seen[path] || seen[path+".lock"] {
			return errors.New("bootstrap root journals or ownership markers overlap or leave the run directory")
		}
		seen[path], seen[path+".lock"] = true, true
	}
	for _, path := range []string{self.ConfigPath, self.ServiceInput.Path} {
		if !bootstrapRootAbsolutePath(path) || seen[path] {
			return errors.New("bootstrap root input is noncanonical or aliases a mutable journal")
		}
	}
	if self.ConfigPath == self.ServiceInput.Path {
		return errors.New("bootstrap root config and service inputs overlap")
	}
	return nil
}

// Literal canonical paths cannot expand environment or escape into an alias.
func bootstrapRootAbsolutePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/" && !strings.ContainsAny(path, "$\x00")
}

// A physical owner-private directory is required before reading or mutating.
// This is a local host boundary, not a distributed or hostile-host fence.
func bootstrapRootDirectory(path string) error {
	if !bootstrapRootAbsolutePath(path) {
		return errors.New("bootstrap root requires a canonical absolute private directory")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return errors.Join(errors.New("bootstrap root directory cannot traverse symlinks"), err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.Join(errors.New("bootstrap root directory is not owner-private"), err)
	}
	return nil
}

// Descriptor checks precede bounded reads. Config, signatures and progress are
// public artifacts, but their local ownership must not be redirected by links.
func readBootstrapRootFile(ctx context.Context, path string, maximum int) ([]byte, string, error) {
	if ctx == nil || !bootstrapRootAbsolutePath(path) || maximum <= 0 {
		return nil, "", errors.New("bootstrap root input context, path or bound is invalid")
	}
	if err := errors.Join(ctx.Err(), bootstrapRootDirectory(filepath.Dir(path))); err != nil {
		return nil, "", err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, "", err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() <= 0 || info.Size() > int64(maximum) {
		return nil, "", errors.Join(errors.New("bootstrap root input is not a bounded private regular file"), err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maximum)+1))
	if err != nil || len(raw) == 0 || len(raw) > maximum || ctx.Err() != nil {
		return nil, "", errors.Join(errors.New("bootstrap root bounded read failed"), err, ctx.Err())
	}
	digest := sha256.Sum256(raw)
	return raw, "sha256:" + hex.EncodeToString(digest[:]), nil
}

// Planning parses exactly the pinned bytes and performs no journal, signer or
// network operation. The new plan is limited to a completable local phase.
func loadBootstrapRootPlan(ctx context.Context, path string) (bootstrapRootPlan, error) {
	raw, digest, err := readBootstrapRootFile(ctx, path, rootServiceStoreLimit)
	if err != nil {
		return bootstrapRootPlan{}, err
	}
	return decodeBootstrapRootPlan(ctx, path, raw, digest)
}

// Composed preparation passes the same immutable bytes whose outer pin it has
// checked. Reopening a pathname here would separate approval from file identity.
func decodeBootstrapRootPlan(ctx context.Context, path string, raw []byte, digest string) (bootstrapRootPlan, error) {
	actual := sha256.Sum256(raw)
	if len(raw) == 0 || len(raw) > rootServiceStoreLimit || digest != "sha256:"+hex.EncodeToString(actual[:]) {
		return bootstrapRootPlan{}, errors.New("bootstrap root config bytes differ from their exact input pin")
	}
	var config bootstrapRootConfig
	if err := decodePlanJson(raw, &config); err != nil {
		return bootstrapRootPlan{}, err
	}
	if config.Schema != bootstrapRootConfigSchema && config.Schema != bootstrapRootPassiveConfigSchema || !planSha256(config.RootService.Sha256) {
		return bootstrapRootPlan{}, errors.New("bootstrap root config schema or service content pin is invalid")
	}
	if err := bootstrapRootDirectory(config.RunDirectory); err != nil {
		return bootstrapRootPlan{}, err
	}
	serviceRaw, serviceHash, err := readBootstrapRootFile(ctx, config.RootService.Path, rootServiceStoreLimit)
	if err != nil || serviceHash != config.RootService.Sha256 {
		return bootstrapRootPlan{}, errors.Join(errors.New("bootstrap root service differs from its exact input pin"), err)
	}
	if config.Schema == bootstrapRootPassiveConfigSchema {
		var service rootPassiveServiceConfig
		if err := decodePlanJson(serviceRaw, &service); err != nil {
			return bootstrapRootPlan{}, err
		}
		plan := bootstrapRootPlan{Schema: bootstrapRootPassivePlanSchema, Phase: bootstrapRootPassivePhase, DeploymentId: config.DeploymentId,
			Network: config.Network, RunDirectory: config.RunDirectory, ConfigPath: path, ConfigSha256: digest, ServiceInput: config.RootService, PassiveService: &service}
		plan.ContentHash = bootstrapRootPlanHash(plan)
		return plan, errors.Join(plan.validate(), ctx.Err())
	}
	var service rootServiceConfig
	if err := decodePlanJson(serviceRaw, &service); err != nil {
		return bootstrapRootPlan{}, err
	}
	plan := bootstrapRootPlan{Schema: bootstrapRootPlanSchema, Phase: bootstrapRootPhase, DeploymentId: config.DeploymentId,
		Network: config.Network, RunDirectory: config.RunDirectory, ConfigPath: path, ConfigSha256: digest, ServiceInput: config.RootService, Service: service}
	plan.ContentHash = bootstrapRootPlanHash(plan)
	if err := errors.Join(plan.validate(), ctx.Err()); err != nil {
		return bootstrapRootPlan{}, err
	}
	return plan, nil
}
