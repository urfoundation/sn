// Operator recovery retains the installed taskworker's original WARP resources.
// An original host declaration and a later incident use separate signatures;
// neither document authorizes installation, signing, SQL repair or key changes.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urnetwork/connect/v2026/durablevolume"
	"github.com/urnetwork/server/v2026/strecovery"
)

const repairOperatorHostSchema = "urnetwork-mainnet-operator-host-v1"
const repairOperatorHostDomain = "urnetwork-mainnet-operator-host-declaration-v1"
const repairOperatorSchema = "urnetwork-mainnet-operator-repair-v1"
const repairOperatorDomain = "urnetwork-mainnet-operator-repair-approval-v1"

var repairOperatorEnvironmentName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,95}$`)
var repairOperatorEnvironmentValue = regexp.MustCompile(`^[a-zA-Z0-9_./:@,+-]+$`)

// The sorted literal environment is the manager's complete explicit Environment
// property. Values cannot contain shell, systemd expansion or quoting syntax.
type repairOperatorEnvironment struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// A complete tree digest includes every directory, physical file identity and
// content hash. Added higher-precedence versions cannot hide behind a file pin.
type repairOperatorTree struct {
	Path         string `json:"path"`
	Sha256       string `json:"sha256"`
	MaximumFiles uint32 `json:"maximum_files"`
	MaximumBytes uint64 `json:"maximum_bytes"`
}

// The original root inode and its separately enrolled durable lease remain
// fixed. A blob root additionally retains its actual existing quota lock.
type repairOperatorRoot struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Blob   bool   `json:"blob"`
}

// Existing journal bytes are signed per incident because ordinary operation
// may advance them. Quota lock identities remain fixed after the start too.
type repairOperatorRetainedFile struct {
	File   planFileReference `json:"file"`
	Device uint64            `json:"device"`
	Inode  uint64            `json:"inode"`
	Quota  bool              `json:"quota_lock"`
}

// Both runtime PostgreSQL pools name an explicit read-only census source. A
// maintenance fallback is still resolved and checked, never assumed identical.
type repairOperatorDatabaseBinding struct {
	Resource string `json:"vault_resource"`
	Source   string `json:"census_source"`
}

// Public identity and the actual parser's public configuration digest bind
// enabled ST, monetary caps, signer roles and upload-resource policy.
type repairOperatorRuntimeSource struct {
	PublicConfigSha256 string                          `json:"public_config_sha256"`
	DeploymentId       string                          `json:"deployment_id"`
	Netuid             uint64                          `json:"netuid"`
	OperatorId         uint64                          `json:"operator_id"`
	DepositSigner      string                          `json:"deposit_signer"`
	RootSigner         string                          `json:"root_signer"`
	ArtifactSigner     string                          `json:"artifact_signer"`
	Databases          []repairOperatorDatabaseBinding `json:"runtime_databases"`
}

// This is an independently reviewed declaration of an already installed host,
// not an installer or a fabricated validator config. Inspector is the existing
// SN read-only storage inspector; Binary is always the actual taskworker.
type repairOperatorHostPlan struct {
	Role                 string                      `json:"role"`
	MachineId            string                      `json:"machine_id"`
	BootId               string                      `json:"boot_id"`
	Unit                 planFileReference           `json:"unit"`
	Binary               planFileReference           `json:"taskworker_binary"`
	Inspector            planFileReference           `json:"storage_inspector_binary"`
	Systemctl            planFileReference           `json:"systemctl"`
	StateDirectory       string                      `json:"state_directory"`
	Uid                  uint32                      `json:"uid"`
	Gid                  uint32                      `json:"gid"`
	Port                 uint16                      `json:"port"`
	Count                uint16                      `json:"worker_count"`
	BatchSize            uint16                      `json:"batch_size"`
	Environment          []repairOperatorEnvironment `json:"environment"`
	Resources            []repairOperatorTree        `json:"warp_resource_trees"`
	WritableRoots        []repairOperatorRoot        `json:"retained_writable_roots"`
	JournalPaths         []string                    `json:"retained_journal_paths"`
	DurableVolumes       durablevolume.Reference     `json:"durable_volumes"`
	RequiredMounts       []string                    `json:"required_mounts"`
	Census               strecovery.Config           `json:"original_signature_sources"`
	Source               repairOperatorRuntimeSource `json:"original_runtime_source"`
	ActivatedAt          time.Time                   `json:"original_activation_at"`
	Generation           repairValidatorGeneration   `json:"original_acknowledged_generation"`
	ExclusiveHostControl bool                        `json:"exclusive_service_and_signing_resources"`
}

// The declaration's verifier is independently supplied by the incident signer.
type repairOperatorHostApproval struct {
	Schema    string                 `json:"schema"`
	Plan      repairOperatorHostPlan `json:"plan"`
	Signature string                 `json:"signature_ed25519"`
}

// A complete original census retains terminal, replacement and uncertain
// attempts. A pending-only monitor projection cannot supply this reference.
type repairOperatorPlan struct {
	OriginalApproval        planFileReference            `json:"original_host_approval"`
	OriginalPublicKey       string                       `json:"original_host_public_key"`
	OriginalCensus          planFileReference            `json:"original_complete_signed_census"`
	OriginalFiles           []repairOperatorRetainedFile `json:"original_retained_files"`
	Predecessor             *repairProcessPredecessor    `json:"previous_repair,omitempty"`
	Previous                repairValidatorGeneration    `json:"previous"`
	IncidentAt              time.Time                    `json:"incident_observed_at"`
	IncidentId              string                       `json:"incident_id"`
	StatePath               string                       `json:"state_path"`
	ValidFrom               time.Time                    `json:"valid_from"`
	ExpiresAt               time.Time                    `json:"expires_at"`
	MaximumStarts           uint8                        `json:"maximum_starts"`
	MaximumObservations     uint32                       `json:"maximum_observations"`
	ReadTimeoutSeconds      uint32                       `json:"read_timeout_seconds"`
	MaximumSampleAgeSeconds uint32                       `json:"maximum_sample_age_seconds"`
}

// No signing key or arbitrary action appears on the public consumer surface.
type repairOperatorApproval struct {
	Schema    string             `json:"schema"`
	Plan      repairOperatorPlan `json:"plan"`
	Signature string             `json:"signature_ed25519"`
}

// The exact role name selects only this fixed taskworker service.
func (self repairOperatorHostPlan) unitName() string {
	return "sn-mainnet-operator-" + self.Role + ".service"
}

// Explicit flags avoid changing authority when upstream defaults evolve.
func (self repairOperatorHostPlan) arguments() string {
	return fmt.Sprintf("--port=%d --count=%d --batch_size=%d", self.Port, self.Count, self.BatchSize)
}

// Taskworker's RequireListenIpPort uses WARP_PORTS only when an explicit host
// IP is present. The HTTP probe and socket census select that same literal port.
func (self repairOperatorHostPlan) statusEndpoint() (string, uint16, error) {
	ipv4, ipv6, port := self.env("WARP_HOST_IPV4"), self.env("WARP_HOST_IPV6"), self.Port
	if ipv4 != "" {
		address, err := netip.ParseAddr(ipv4)
		if err != nil || !address.Is4() {
			return "", 0, errors.New("operator original listener IPv4 input differs")
		}
	}
	if ipv6 != "" {
		address, err := netip.ParseAddr(ipv6)
		if err != nil || !address.Is6() {
			return "", 0, errors.New("operator original listener IPv6 input differs")
		}
	}
	if ipv4 != "" || ipv6 != "" {
		mapping := map[uint16]uint16{}
		for _, pair := range strings.Split(self.env("WARP_PORTS"), ",") {
			key, value, found := strings.Cut(pair, ":")
			service, serviceErr := strconv.ParseUint(key, 10, 16)
			host, hostErr := strconv.ParseUint(value, 10, 16)
			if !found || serviceErr != nil || hostErr != nil || service == 0 || host == 0 || mapping[uint16(service)] != 0 {
				return "", 0, errors.New("operator original listener port mapping is incomplete or ambiguous")
			}
			mapping[uint16(service)] = uint16(host)
		}
		port = mapping[self.Port]
		if port == 0 {
			return "", 0, errors.New("operator original taskworker port has no declared host mapping")
		}
	}
	if ipv4 == "" || ipv4 == "0.0.0.0" {
		ipv4 = "127.0.0.1"
	}
	return ipv4, port, nil
}

// Sorted canonical words match systemd's literal Environment property.
func (self repairOperatorHostPlan) environment() string {
	values := make([]string, 0, len(self.Environment))
	for _, value := range self.Environment {
		values = append(values, value.Name+"="+value.Value)
	}
	return strings.Join(values, " ")
}

// Lookup never reads the repair controller's ambient environment.
func (self repairOperatorHostPlan) env(name string) string {
	for _, value := range self.Environment {
		if value.Name == name {
			return value.Value
		}
	}
	return ""
}

// This installed profile has no shell, init-tasks, restart policy or mutable
// environment file. Database scheduling stays in the original taskworker.
func (self repairOperatorHostPlan) render() []byte {
	return []byte(fmt.Sprintf("[Unit]\nDescription=Approved mainnet operator %s\nDefaultDependencies=no\nAfter=network-online.target\n\n[Service]\nType=exec\nUser=%d\nGroup=%d\nWorkingDirectory=%s\nEnvironment=%s\nExecStart=%s %s\nRestart=no\nKillMode=control-group\nSendSIGKILL=yes\nTimeoutStartSec=30\nTimeoutStopSec=30\nUMask=0077\nNoNewPrivileges=yes\nDelegate=no\n\n[Install]\nWantedBy=multi-user.target\n", self.Role, self.Uid, self.Gid, self.StateDirectory, self.environment(), self.Binary.Path, self.arguments()))
}

// Signature generation is deliberately absent; tests and independent operators
// may construct the bytes to be reviewed through their separate signing owner.
func (self repairOperatorHostApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(repairOperatorHostDomain+"\x00"), raw...), err
}

// All resource roots are explicit, nonoverlapping and finite. The complete
// original source inventory is independent of the later incident selection.
func (self repairOperatorHostApproval) validate(key string) error {
	p := self.Plan
	if self.Schema != repairOperatorHostSchema || !rootCanonicalHash(key) || !monitorRolePattern.MatchString(p.Role) || !p.ExclusiveHostControl || p.Uid == 0 || p.Gid == 0 || p.Port == 0 || p.Count < 1 || p.Count > 128 || p.BatchSize < 1 || p.BatchSize > 1024 || !repairValidatorHex(p.MachineId, 16) || len(p.BootId) != 36 || p.BootId[8] != '-' || p.BootId[13] != '-' || p.BootId[18] != '-' || p.BootId[23] != '-' || !repairValidatorHex(strings.ReplaceAll(p.BootId, "-", ""), 16) || !repairValidatorHex(p.Generation.InvocationId, 16) || p.Generation.Pid <= 1 || p.Generation.StartedUsec == 0 || p.ActivatedAt.IsZero() {
		return errors.New("operator original host, exclusive control or acknowledged generation is incomplete")
	}
	if len(p.Environment) < 12 || len(p.Environment) > 128 || len(p.Resources) != 3 || len(p.WritableRoots) < 1 || len(p.WritableRoots) > 32 || len(p.JournalPaths) > 256 || len(p.RequiredMounts) > 8 {
		return errors.New("operator original resource census exceeds its finite profile")
	}
	previous := ""
	for _, value := range p.Environment {
		if !repairOperatorEnvironmentName.MatchString(value.Name) || len(value.Value) < 1 || len(value.Value) > 2048 || !repairOperatorEnvironmentValue.MatchString(value.Value) || value.Name <= previous {
			return errors.New("operator environment is not an exact canonical literal census")
		}
		previous = value.Name
	}
	for _, name := range []string{"WARP_HOST", "WARP_BLOCK", "WARP_VERSION", "WARP_CONFIG_VERSION", "WARP_HOME", "WARP_VAULT_HOME", "WARP_CONFIG_HOME", "WARP_SITE_HOME"} {
		if p.env(name) == "" {
			return errors.New("operator original WARP environment is incomplete")
		}
	}
	if p.env("WARP_SERVICE") != "taskworker" || p.env("WARP_ENV") != "main" || p.env("URNETWORK_ST_PROFILE") != "mainnet" || p.env("LANG") != "C" || p.env("INVOCATION_ID") != "" {
		return errors.New("operator original environment does not select the mainnet taskworker")
	}
	if _, _, err := p.statusEndpoint(); err != nil {
		return err
	}
	paths := map[string]bool{}
	for _, ref := range []planFileReference{p.Unit, p.Binary, p.Inspector, p.Systemctl, {Path: p.DurableVolumes.Path, Sha256: p.DurableVolumes.Sha256}} {
		if !repairValidatorPath(ref.Path) || !planSha256(ref.Sha256) || paths[ref.Path] {
			return errors.New("operator original release pins overlap or are incomplete")
		}
		paths[ref.Path] = true
	}
	if filepath.Base(p.Unit.Path) != p.unitName() || p.Unit.Sha256 != monitorReadDigest(p.render()) || !repairValidatorPath(p.StateDirectory) {
		return errors.New("operator original installed unit differs from its fixed profile")
	}
	resourcePaths := []string{p.env("WARP_VAULT_HOME"), p.env("WARP_CONFIG_HOME"), p.env("WARP_SITE_HOME")}
	slices.Sort(resourcePaths)
	for index, tree := range p.Resources {
		if tree.Path != resourcePaths[index] || !repairValidatorPath(tree.Path) || !planSha256(tree.Sha256) || tree.MaximumFiles < 1 || tree.MaximumFiles > 16384 || tree.MaximumBytes < 1 || tree.MaximumBytes > 64*1024*1024 {
			return errors.New("operator WARP closure is not the complete three bounded resource roots")
		}
		for _, prior := range p.Resources[:index] {
			if rootPassiveHostPathContains(prior.Path, tree.Path) || rootPassiveHostPathContains(tree.Path, prior.Path) {
				return errors.New("operator WARP resource roots overlap")
			}
		}
	}
	previous, stateFound := "", false
	for _, root := range p.WritableRoots {
		if !repairValidatorPath(root.Path) || root.Path <= previous || root.Inode == 0 || root.Device == 0 {
			return errors.New("operator original writable root identity is incomplete")
		}
		for _, tree := range p.Resources {
			if rootPassiveHostPathContains(tree.Path, root.Path) || rootPassiveHostPathContains(root.Path, tree.Path) {
				return errors.New("operator writable root overlaps original immutable resources")
			}
		}
		for _, prior := range p.WritableRoots {
			if prior.Path != root.Path && rootPassiveHostPathContains(prior.Path, root.Path) {
				return errors.New("operator original writable roots overlap")
			}
		}
		stateFound = stateFound || root.Path == p.StateDirectory
		previous = root.Path
	}
	if !stateFound {
		return errors.New("operator working directory is absent from retained root custody")
	}
	previous = ""
	for _, path := range p.JournalPaths {
		if !repairValidatorPath(path) || path <= previous || !p.ownsPath(path) {
			return errors.New("operator retained journal census is not canonical original custody")
		}
		previous = path
	}
	previous = ""
	for _, mount := range p.RequiredMounts {
		if !repairValidatorMountPattern.MatchString(mount) || len(mount) > 128 || mount <= previous {
			return errors.New("operator prerequisite mount census differs")
		}
		previous = mount
	}
	if err := p.Census.Validate(); err != nil || p.Census.ChainId != mainnetEvmChainId {
		return errors.Join(errors.New("operator original signature source declaration differs"), err)
	}
	if !planSha256(p.Source.PublicConfigSha256) || p.Source.DeploymentId == "" || len(p.Source.DeploymentId) > 128 || p.Source.Netuid == 0 || p.Source.Netuid > 65535 || p.Source.OperatorId == 0 || len(p.Source.Databases) != 2 || p.Source.Databases[0].Resource != "pg.yml" || p.Source.Databases[1].Resource != "pg_maintenance.yml" {
		return errors.New("operator original enabled runtime source declaration is incomplete")
	}
	signers := map[string]bool{}
	for _, address := range []string{p.Source.DepositSigner, p.Source.RootSigner, p.Source.ArtifactSigner} {
		if len(address) != 42 || !repairValidatorHex(strings.TrimPrefix(address, "0x"), 20) || !strings.HasPrefix(address, "0x") || signers[address] {
			return errors.New("operator original signing-resource roles are not distinct canonical addresses")
		}
		signers[address] = true
	}
	for _, binding := range p.Source.Databases {
		found := false
		for _, source := range p.Census.Databases {
			if source.Id != binding.Source {
				continue
			}
			found = true
			for _, address := range []string{p.Source.DepositSigner, p.Source.RootSigner} {
				covered := false
				for _, role := range p.Census.Roles {
					covered = covered || role.Address == address && slices.Contains(source.Roles, role.Id)
				}
				if !covered {
					return errors.New("operator runtime database omits an actual transaction signing role")
				}
			}
		}
		if !found {
			return errors.New("operator runtime database has no original complete census source")
		}
	}
	message, err := self.signingBytes()
	return errors.Join(err, repairOperatorVerify(key, self.Signature, message))
}

// The source roots are already enrolled; a repair cannot add another owner.
func (self repairOperatorHostPlan) ownsPath(path string) bool {
	for _, root := range self.WritableRoots {
		if path != root.Path && rootPassiveHostPathContains(root.Path, path) {
			return true
		}
	}
	return false
}

// Separate domains prevent an original host declaration from granting repair.
func repairOperatorVerify(key, signature string, message []byte) error {
	if !rootCanonicalHash(key) {
		return errors.New("operator independent verifier is invalid")
	}
	public, _ := hex.DecodeString(key[2:])
	raw, err := hex.DecodeString(signature)
	if err != nil || len(raw) != ed25519.SignatureSize || hex.EncodeToString(raw) != signature || !ed25519.Verify(public, message, raw) {
		return errors.New("operator independent signature differs")
	}
	return nil
}

// A new path cannot manufacture another incident for the same generation.
func (self repairOperatorPlan) incidentId() string {
	return rootObjectHash(struct {
		Schema      string                       `json:"schema"`
		Host        planFileReference            `json:"host"`
		Census      planFileReference            `json:"census"`
		Files       []repairOperatorRetainedFile `json:"files"`
		Predecessor *repairProcessPredecessor    `json:"previous_repair,omitempty"`
		Previous    repairValidatorGeneration    `json:"previous"`
		At          time.Time                    `json:"at"`
	}{repairOperatorSchema, self.OriginalApproval, self.OriginalCensus, self.OriginalFiles, self.Predecessor, self.Previous, self.IncidentAt})
}

// Five minutes is the default total read owner, never a per-source refill.
func (self repairOperatorPlan) timeoutSeconds() uint32 {
	if self.ReadTimeoutSeconds == 0 {
		return 300
	}
	return self.ReadTimeoutSeconds
}

// Signature bytes retain exact original references and all finite allowances.
func (self repairOperatorApproval) signingBytes() ([]byte, error) {
	self.Signature = ""
	raw, err := json.Marshal(self)
	return append([]byte(repairOperatorDomain+"\x00"), raw...), err
}

// A fresh repair is one independently signed stopped-generation incident.
func (self repairOperatorApproval) validate(key string) error {
	p := self.Plan
	if self.Schema != repairOperatorSchema || !rootCanonicalHash(p.OriginalPublicKey) || p.MaximumStarts != 1 || p.MaximumObservations < 1 || p.MaximumObservations > 1024 || p.timeoutSeconds() < 60 || p.timeoutSeconds() > 900 || p.MaximumSampleAgeSeconds < 1 || p.MaximumSampleAgeSeconds > 600 || p.IncidentAt.IsZero() || p.IncidentAt.After(p.ValidFrom) || !p.ExpiresAt.After(p.ValidFrom) || p.ExpiresAt.Sub(p.ValidFrom) > 24*time.Hour || p.IncidentId != p.incidentId() || !repairValidatorHex(p.Previous.InvocationId, 16) || p.Previous.Pid <= 1 || p.Previous.StartedUsec == 0 || len(p.OriginalFiles) > 288 {
		return errors.New("operator repair requires one original incident and finite process allowance")
	}
	refs := []planFileReference{p.OriginalApproval, p.OriginalCensus}
	if p.Predecessor != nil {
		if !rootCanonicalHash(p.Predecessor.PublicKey) {
			return errors.New("operator previous repair verifier is absent")
		}
		refs = append(refs, p.Predecessor.Approval, p.Predecessor.Journal)
	}
	paths := map[string]bool{}
	for _, ref := range refs {
		if !repairValidatorPath(ref.Path) || !planSha256(ref.Sha256) || paths[ref.Path] {
			return errors.New("operator repair original references overlap or are incomplete")
		}
		paths[ref.Path], paths[ref.Path+".lock"] = true, true
	}
	previous := ""
	for _, retained := range p.OriginalFiles {
		if !repairValidatorPath(retained.File.Path) || !planSha256(retained.File.Sha256) || retained.File.Path <= previous || retained.Inode == 0 || retained.Device == 0 || paths[retained.File.Path] {
			return errors.New("operator repair retained file census differs")
		}
		previous = retained.File.Path
		paths[retained.File.Path] = true
	}
	if !repairValidatorPath(p.StatePath) || paths[p.StatePath] || paths[p.StatePath+".lock"] {
		return errors.New("operator repair state overlaps original custody")
	}
	message, err := self.signingBytes()
	return errors.Join(err, repairOperatorVerify(key, self.Signature, message))
}
