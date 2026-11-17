// Actual Server resolution and ST parsing join the original host declaration
// to consumed database, signer and blob resources without starting a worker.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/url"
	"strings"

	"github.com/urnetwork/server/v2026"
	"github.com/urnetwork/server/v2026/controller"
	"github.com/urnetwork/server/v2026/model"
	"gopkg.in/yaml.v3"
)

// The same production resolver selects the first literal/layer/version result.
// Reading is still under the original protected complete-tree authority.
func readRepairOperatorResource(ctx context.Context, host *repairValidatorHost, plan repairOperatorHostPlan, root, name string) ([]byte, error) {
	paths, err := server.ResolveResourcePathsWithAccess(root, plan.env("WARP_ENV"), name, func(operation, path string) error {
		return inspectRepairOperatorLookupAccess(ctx, host, plan, operation, path)
	})
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 || !rootPassiveHostPathContains(root, paths[0]) {
		return nil, errors.Join(errRpcIntegrity, errors.New("operator actual resolver selected no protected resource"))
	}
	if err := inspectRepairOperatorResourceAccess(ctx, host, plan, paths[0]); err != nil {
		return nil, err
	}
	raw, _, err := readRepairOperatorFile(ctx, host, paths[0], host.rootUid, 2*1024*1024)
	return raw, err
}

// This private YAML map never appears in a diagnostic or retained output.
func decodeRepairOperatorResource(raw []byte) (map[string]any, error) {
	var value map[string]any
	if err := yaml.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, errors.Join(errRpcIntegrity, errors.New("operator selected resource is not a valid mapping"))
	}
	return value, nil
}

// Actual Config settings all/host are shallow merged before Site settings,
// matching Server.GetSettings. Complete tree checks already bound env_vars.
func repairOperatorRoutes(ctx context.Context, host *repairValidatorHost, plan repairOperatorHostPlan) (map[string]string, error) {
	settings := map[string]any{}
	for _, kind := range []string{"config", "site"} {
		root := plan.env("WARP_CONFIG_HOME")
		if kind == "site" {
			root = plan.env("WARP_SITE_HOME")
		}
		raw, err := readRepairOperatorResource(ctx, host, plan, root, "settings.yml")
		if errors.Is(err, server.ErrResourceNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		value, err := decodeRepairOperatorResource(raw)
		if err != nil {
			return nil, err
		}
		if kind == "site" {
			maps.Copy(settings, value)
		} else {
			for _, key := range []string{"all", plan.env("WARP_HOST")} {
				if section, ok := value[key].(map[string]any); ok {
					maps.Copy(settings, section)
				}
			}
		}
	}
	result := map[string]string{}
	if values, ok := settings["routes"].(map[string]any); ok {
		for name, raw := range values {
			value, ok := raw.(string)
			if !ok {
				return nil, errors.Join(errRpcIntegrity, errors.New("operator original route is not a literal host"))
			}
			result[name] = value
		}
	}
	return result, nil
}

// The production interpolation grammar uses only explicit original inputs.
func repairOperatorString(value any, plan repairOperatorHostPlan) (string, error) {
	raw, ok := value.(string)
	if !ok {
		return "", errors.Join(errRpcIntegrity, errors.New("operator configured source is not a literal string"))
	}
	missing := false
	result := repairOperatorInterpolation.ReplaceAllStringFunc(raw, func(match string) string {
		parts := repairOperatorInterpolation.FindStringSubmatch(match)
		value := plan.env(parts[1])
		missing = missing || value == ""
		return value
	})
	if missing {
		return "", errors.Join(errRpcIntegrity, errors.New("operator configured source requires an undeclared environment input"))
	}
	return result, nil
}

// The runtime's actual mainnet connection selection prefers explicit RPC URLs.
// URL bytes stay private; only the existing config parser receives this list.
func repairOperatorRpcUrls(value map[string]any, plan repairOperatorHostPlan) ([]string, error) {
	urls := []string{}
	appendValue := func(raw any) error {
		text, err := repairOperatorString(raw, plan)
		if err != nil {
			return err
		}
		if text = strings.TrimSpace(text); text != "" {
			urls = append(urls, text)
		}
		return nil
	}
	if raw, exists := value["rpc_urls"]; exists {
		switch raw := raw.(type) {
		case string:
			if err := appendValue(raw); err != nil {
				return nil, err
			}
		case []any:
			for _, item := range raw {
				if err := appendValue(item); err != nil {
					return nil, err
				}
			}
		default:
			return nil, errors.Join(errRpcIntegrity, errors.New("operator RPC source has an invalid shape"))
		}
	}
	if len(urls) > 0 {
		return urls, nil
	}
	authority, err := repairOperatorString(value["authority"], plan)
	if err != nil || strings.TrimSpace(authority) == "" {
		return nil, errors.Join(errRpcIntegrity, errors.New("operator enabled ST has no actual RPC connection"), err)
	}
	authority = strings.TrimSpace(authority)
	if !strings.Contains(authority, ":") {
		authority += ":9944"
	}
	scheme := "http"
	if tls, _ := value["tls"].(bool); tls {
		scheme = "https"
	}
	return []string{scheme + "://" + authority}, nil
}

// Exact ST parsing validates all original monetary caps and distinct keys.
// Only public key addresses and the safe public-config digest leave its owner.
func inspectRepairOperatorSt(raw []byte, plan repairOperatorHostPlan) (*controller.StConfigInspection, error) {
	value, err := decodeRepairOperatorResource(raw)
	if err != nil {
		return nil, err
	}
	urls, err := repairOperatorRpcUrls(value, plan)
	if err != nil {
		return nil, err
	}
	result, err := controller.InspectStConfigBytes(raw, "mainnet", urls)
	if err != nil {
		return nil, errors.Join(errRpcIntegrity, err)
	}
	if result == nil || !result.Enabled || result.Profile != "mainnet" || result.ChainId != plan.Census.ChainId || "0x"+hex.EncodeToString(result.GenesisHash[:]) != plan.Census.Genesis || result.DeploymentId != plan.Source.DeploymentId || result.Netuid != plan.Source.Netuid || result.NoId != plan.Source.OperatorId || "sha256:"+hex.EncodeToString(result.PublicConfigSha256[:]) != plan.Source.PublicConfigSha256 || strings.ToLower(result.DepositKeyAddress.Hex()) != plan.Source.DepositSigner || strings.ToLower(result.RootKeyAddress.Hex()) != plan.Source.RootSigner || strings.ToLower(result.ArtifactKeyAddress.Hex()) != plan.Source.ArtifactSigner || result.OpsKeyAddress != result.RootKeyAddress {
		return nil, errors.Join(errRpcIntegrity, errors.New("operator actual ST source, signers or original monetary configuration differs"))
	}
	return result, nil
}

// Main and maintenance pools must read the same explicitly named databases as
// their independent all-status census sources. PostgreSQL does not use the
// WARP blob route map; its URL must identify the same literal host and port.
func inspectRepairOperatorDatabase(ctx context.Context, host *repairValidatorHost, plan repairOperatorHostPlan, binding repairOperatorDatabaseBinding) error {
	raw, err := readRepairOperatorResource(ctx, host, plan, plan.env("WARP_VAULT_HOME"), binding.Resource)
	if errors.Is(err, server.ErrResourceNotFound) && binding.Resource == "pg_maintenance.yml" {
		raw, err = readRepairOperatorResource(ctx, host, plan, plan.env("WARP_VAULT_HOME"), "pg.yml")
	}
	if err != nil {
		return err
	}
	value, err := decodeRepairOperatorResource(raw)
	if err != nil {
		return err
	}
	authority, authorityErr := repairOperatorString(value["authority"], plan)
	database, databaseErr := repairOperatorString(value["db"], plan)
	user, userErr := repairOperatorString(value["user"], plan)
	password, passwordErr := repairOperatorString(value["password"], plan)
	if err := errors.Join(authorityErr, databaseErr, userErr, passwordErr); err != nil {
		return err
	}
	if database == "" || user == "" || password == "" {
		return errors.Join(errRpcIntegrity, errors.New("operator runtime database resource is incomplete"))
	}
	hostname, port, err := net.SplitHostPort(authority)
	if err != nil || hostname == "" || port == "" {
		return errors.Join(errRpcIntegrity, errors.New("operator runtime database authority must select an explicit port"))
	}
	// The runtime formats these scalars directly into its pgx URL. Reject
	// delimiter or query injection that would select another connection after
	// parsing; no credential value appears in the refusal.
	runtime, err := url.Parse(fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=disable", user, password, authority, database))
	if err != nil || runtime.Hostname() != hostname || runtime.Port() != port || runtime.Path != "/"+database || runtime.RawQuery != "sslmode=disable" || runtime.Fragment != "" || runtime.User == nil || runtime.User.Username() != user {
		return errors.Join(errRpcIntegrity, errors.New("operator runtime database URL changes its literal source identity"))
	}
	for _, source := range plan.Census.Databases {
		if source.Id != binding.Source {
			continue
		}
		connection, _, err := readRepairOperatorFile(ctx, host, source.Connection.Path, host.rootUid, 16*1024)
		if err != nil {
			return err
		}
		if monitorReadDigest(connection) != source.Connection.Sha256 {
			return errors.Join(errRpcIntegrity, errors.New("operator read-only database connection pin changed"))
		}
		selected, err := url.Parse(strings.TrimSpace(string(connection)))
		if err != nil || selected.Scheme != "postgres" && selected.Scheme != "postgresql" || selected.Fragment != "" || selected.Query().Get("sslmode") == "" || selected.Hostname() != hostname || selected.Port() != port || selected.Path != "/"+database || selected.User == nil || selected.User.Username() == user {
			return errors.Join(errRpcIntegrity, errors.New("operator actual database and independent census source differ"))
		}
		for key, values := range selected.Query() {
			if key != "sslmode" || len(values) != 1 {
				return errors.Join(errRpcIntegrity, errors.New("operator census connection has an undeclared external input"))
			}
		}
		return nil
	}
	return errors.Join(errRpcIntegrity, errors.New("operator actual database is absent from complete original census"))
}

// Local blobs must retain their real declared root, original quota inode and
// original durable-volume policy. Remote blobs retain their exact pinned vault
// configuration and gain no local or remote repair authority here.
func inspectRepairOperatorBlob(raw []byte, plan repairOperatorHostPlan, routes map[string]string) error {
	value, err := decodeRepairOperatorResource(raw)
	if err != nil {
		return err
	}
	authority, _ := value["authority"].(string)
	if authority != "" {
		authority, err = repairOperatorString(authority, plan)
		if err != nil {
			return err
		}
		hostname, port := authority, ""
		if index := strings.LastIndex(authority, ":"); index >= 0 {
			hostname, port = authority[:index], authority[index+1:]
		}
		if route := routes[hostname]; route != "" {
			authority = route
			if port != "" {
				authority += ":" + port
			}
		}
	}
	if authority != "" && !strings.EqualFold(authority, "local") {
		for _, name := range []string{"access_key", "secret_key", "bucket"} {
			if raw, _ := value[name].(string); raw == "" {
				return errors.Join(errRpcIntegrity, errors.New("operator remote blob resource is incomplete"))
			}
		}
		return nil
	}
	path, _ := value["path"].(string)
	capacity, ok := value["max_bytes"].(int)
	declaration, declared := value["durable_volumes"].(map[string]any)
	if !repairValidatorPath(path) || !ok || capacity <= 0 || !declared || declaration["path"] != plan.DurableVolumes.Path || declaration["sha256"] != plan.DurableVolumes.Sha256 {
		return errors.Join(errRpcIntegrity, errors.New("operator actual local blob quota or durable root is undeclared"))
	}
	for _, root := range plan.WritableRoots {
		if root.Path == path && root.Blob {
			return nil
		}
	}
	return errors.Join(errRpcIntegrity, errors.New("operator actual local blob root is absent from original quota custody"))
}

// An absent optional live-session signer remains absent. If configured, the
// taskworker must be able to consume its original bytes under the production
// parser. The complete tree pin retains its independent key and authority;
// neither this read nor taskworker readiness admits it to an earning roster.
func inspectRepairOperatorSessionSource(ctx context.Context, host *repairValidatorHost, plan repairOperatorHostPlan) error {
	raw, err := readRepairOperatorResource(ctx, host, plan, plan.env("WARP_VAULT_HOME"), "provider_work_session.json")
	if errors.Is(err, server.ErrResourceNotFound) {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if _, err := model.InspectProviderWorkSessionSourceBytes(raw); err != nil {
		return errors.Join(errRpcIntegrity, errors.New("operator original optional session source is invalid"), err)
	}
	return ctx.Err()
}

// This is the source-adapter boundary used before any claim or process effect.
// Existing Server resolution and ST configuration validation remain operative.
func (self *repairOperatorCustody) source(ctx context.Context) error {
	p := self.envelope.original.Plan
	routes, err := repairOperatorRoutes(ctx, self.host, p)
	if err != nil {
		return err
	}
	raw, err := readRepairOperatorResource(ctx, self.host, p, p.env("WARP_VAULT_HOME"), "st.yml")
	if err != nil {
		return err
	}
	if _, err := inspectRepairOperatorSt(raw, p); err != nil {
		return err
	}
	if err := inspectRepairOperatorSessionSource(ctx, self.host, p); err != nil {
		return err
	}
	for _, binding := range p.Source.Databases {
		if err := inspectRepairOperatorDatabase(ctx, self.host, p, binding); err != nil {
			return err
		}
	}
	for _, resource := range []struct{ root, name string }{{p.env("WARP_VAULT_HOME"), "redis.yml"}, {p.env("WARP_CONFIG_HOME"), "redis.yml"}, {p.env("WARP_CONFIG_HOME"), "db.yml"}} {
		raw, err := readRepairOperatorResource(ctx, self.host, p, resource.root, resource.name)
		if err != nil {
			return err
		}
		if _, err := decodeRepairOperatorResource(raw); err != nil {
			return err
		}
	}
	raw, err = readRepairOperatorResource(ctx, self.host, p, p.env("WARP_VAULT_HOME"), "minio.yml")
	if err != nil {
		return err
	}
	if err := inspectRepairOperatorBlob(raw, p, routes); err != nil {
		return err
	}
	return ctx.Err()
}
