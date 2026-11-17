package config

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// AzureSubscription is one Azure subscription to draw, in the commercial or
// Government cloud. Only SourceSteampipe reads Azure.
type AzureSubscription struct {
	Source         Source `json:"source,omitempty"`
	SubscriptionID string `json:"subscription_id"`
	// TenantID names the subscription's Entra ID tenant. The steampipe
	// source does not need it.
	TenantID  string         `json:"tenant_id,omitempty"`
	Regions   []string       `json:"regions"`
	Resources AzureResources `json:"resources"`
}

// AzureResources holds one field per Azure service.
type AzureResources struct {
	// IncludeAll reads every resource type the source can supply, as
	// Resources.IncludeAll does for AWS.
	IncludeAll bool `json:"include_all,omitempty"`

	Compute          *AzureComputeResources          `json:"compute,omitempty"`
	Network          *AzureNetworkResources          `json:"network,omitempty"`
	KeyVault         *AzureKeyVaultResources         `json:"keyvault,omitempty"`
	AppConfiguration *AzureAppConfigurationResources `json:"appconfiguration,omitempty"`
}

// AzureComputeResources holds the compute resource types, all primary.
type AzureComputeResources struct {
	VirtualMachines         *AzureResourceQuery `json:"virtual_machines,omitempty"`
	VirtualMachineScaleSets *AzureResourceQuery `json:"virtual_machine_scale_sets,omitempty"`
	Disks                   *AzureResourceQuery `json:"disks,omitempty"`
}

// AzureNetworkResources holds the network resource types. Container types
// are always read, and their query here only narrows the results.
type AzureNetworkResources struct {
	// Primary resources.
	Firewalls        *AzureResourceQuery `json:"firewalls,omitempty"`
	PrivateEndpoints *AzureResourceQuery `json:"private_endpoints,omitempty"`

	// Container resources.
	VirtualNetworks       *AzureResourceQuery `json:"virtual_networks,omitempty"`
	Subnets               *AzureResourceQuery `json:"subnets,omitempty"`
	NetworkSecurityGroups *AzureResourceQuery `json:"network_security_groups,omitempty"`
	NetworkInterfaces     *AzureResourceQuery `json:"network_interfaces,omitempty"`
	PublicIPs             *AzureResourceQuery `json:"public_ips,omitempty"`
	NATGateways           *AzureResourceQuery `json:"nat_gateways,omitempty"`
}

// AzureKeyVaultResources holds the Key Vault resource types. Only vaults are
// read: the key, secret, and certificate tables are never read.
type AzureKeyVaultResources struct {
	Vaults *AzureResourceQuery `json:"vaults,omitempty"`
}

// AzureAppConfigurationResources holds the App Configuration resource types.
type AzureAppConfigurationResources struct {
	Stores *AzureResourceQuery `json:"stores,omitempty"`
}

// AzureResourceQuery selects Azure resources. Its conditions are ANDed, and
// a query with none selects every resource of its type. IDs and resource
// groups match case-insensitively, as Azure treats them.
type AzureResourceQuery struct {
	IDs            []string          `json:"ids,omitempty"`
	ResourceGroups []string          `json:"resource_groups,omitempty"`
	Tags           map[string]string `json:"tags,omitempty"`
	// ShowUnconnectedResources, Hide, and HideChildren mean what they mean
	// on ResourceQuery.
	ShowUnconnectedResources *bool `json:"show_unconnected_resources,omitempty"`
	Hide                     bool  `json:"hide,omitempty"`
	HideChildren             bool  `json:"hide_children,omitempty"`
}

// Subscription returns the configured Azure subscription and its key under
// cloud, or nil when the config names an AWS account instead.
func (c *Config) Subscription() (string, *AzureSubscription) {
	if c.Cloud.AzureGov != nil {
		return "azure_gov", c.Cloud.AzureGov
	}
	return "azure", c.Cloud.Azure
}

// AzureQuery adapts a field picker on one Azure service to a lookup on
// AzureResources, for code that holds per-type queries as functions. It
// returns nil when the config has no section for the service.
func AzureQuery[S any](
	service func(*AzureResources) *S, pick func(*S) *AzureResourceQuery,
) func(*AzureResources) *AzureResourceQuery {
	return func(r *AzureResources) *AzureResourceQuery {
		s := service(r)
		if s == nil {
			return nil
		}
		return pick(s)
	}
}

// Service pickers for AzureQuery.
var (
	AzureCompute   = func(r *AzureResources) *AzureComputeResources { return r.Compute }
	AzureNetwork   = func(r *AzureResources) *AzureNetworkResources { return r.Network }
	AzureKeyVault  = func(r *AzureResources) *AzureKeyVaultResources { return r.KeyVault }
	AzureAppConfig = func(r *AzureResources) *AzureAppConfigurationResources { return r.AppConfiguration }
)

// guid matches an Azure subscription or tenant ID.
var guid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validate checks one subscription. Errors start with the failing key, so
// the caller can prefix the subscription's path.
func (s *AzureSubscription) validate() error {
	if s.Source == "" {
		s.Source = SourceSteampipe
	}
	if s.Source != SourceSteampipe {
		return fmt.Errorf("source: Azure is read only through \"steampipe\", not %q", s.Source)
	}
	if !guid.MatchString(s.SubscriptionID) {
		return fmt.Errorf("subscription_id: %q is not a subscription ID (a GUID)", s.SubscriptionID)
	}
	if s.TenantID != "" && !guid.MatchString(s.TenantID) {
		return fmt.Errorf("tenant_id: %q is not a tenant ID (a GUID)", s.TenantID)
	}
	if len(s.Regions) == 0 {
		return errors.New("regions: empty")
	}
	return s.Resources.validate()
}

// validate checks every resource query.
func (r AzureResources) validate() error {
	queries := map[string]*AzureResourceQuery{}
	if c := r.Compute; c != nil {
		queries["compute.virtual_machines"] = c.VirtualMachines
		queries["compute.virtual_machine_scale_sets"] = c.VirtualMachineScaleSets
		queries["compute.disks"] = c.Disks
	}
	if n := r.Network; n != nil {
		for name, q := range map[string]*AzureResourceQuery{
			"firewalls":               n.Firewalls,
			"private_endpoints":       n.PrivateEndpoints,
			"virtual_networks":        n.VirtualNetworks,
			"subnets":                 n.Subnets,
			"network_security_groups": n.NetworkSecurityGroups,
			"network_interfaces":      n.NetworkInterfaces,
			"public_ips":              n.PublicIPs,
			"nat_gateways":            n.NATGateways,
		} {
			queries["network."+name] = q
		}
	}
	if r.KeyVault != nil {
		queries["keyvault.vaults"] = r.KeyVault.Vaults
	}
	if r.AppConfiguration != nil {
		queries["appconfiguration.stores"] = r.AppConfiguration.Stores
	}
	for _, name := range slices.Sorted(maps.Keys(queries)) {
		q := queries[name]
		if q == nil {
			continue
		}
		if len(q.IDs) > 0 && q.ShowUnconnectedResources != nil {
			return fmt.Errorf(
				"resources.%s: ids and show_unconnected_resources cannot be combined; ids always draws the listed resources",
				name,
			)
		}
		for _, id := range q.IDs {
			if !strings.HasPrefix(id, "/subscriptions/") {
				return fmt.Errorf("resources.%s.ids: %q is not an ARM resource ID (/subscriptions/...)", name, id)
			}
		}
	}
	return nil
}
