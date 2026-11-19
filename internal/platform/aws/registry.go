package aws

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"diagram/internal/config"
)

// ResourceType is one AWS resource type the config can name. The registry
// drives the config keys, their validation, and the per-type selection rules
// in Finish. Sources key their readers by Type.
type ResourceType struct {
	// Service and Key are the type's path in the config file:
	// resources.<Service>.<Key>.
	Service, Key string
	// Type is the record type, as CloudFormation names it.
	Type string
	// Primary types are collected only when the config names them. Other
	// types are context, such as VPCs and subnets, and are always collected.
	Primary bool
	// Prunable types are dropped when unconnected and the config asks for
	// that.
	Prunable bool
	// NoFilters types reject query filters, because their list calls take
	// none.
	NoFilters bool
}

// Name is the type's path under resources, as logs and errors print it.
func (t ResourceType) Name() string {
	return t.Service + "." + t.Key
}

// Query returns the config's query for the type, or nil when the config does
// not name it.
func (t ResourceType) Query(r *config.Resources) *config.ResourceQuery {
	return r.Query(t.Service, t.Key)
}

// Registry lists every resource type the config can name, by service and
// then key.
var Registry = []ResourceType{
	{Service: "ec2", Key: "customer_gateways", Type: TypeCustomerGateway, Prunable: true},
	{Service: "ec2", Key: "instances", Type: TypeInstance, Primary: true},
	{Service: "ec2", Key: "internet_gateways", Type: TypeInternetGateway, Prunable: true},
	{Service: "ec2", Key: "nat_gateways", Type: TypeNATGateway, Prunable: true},
	{Service: "ec2", Key: "prefix_lists", Type: TypePrefixList, Prunable: true},
	{Service: "ec2", Key: "security_groups", Type: TypeSecurityGroup, Prunable: true},
	{Service: "ec2", Key: "subnets", Type: TypeSubnet, Prunable: true},
	{Service: "ec2", Key: "transit_gateways", Type: TypeTransitGateway, Prunable: true},
	{Service: "ec2", Key: "vpcs", Type: TypeVPC, Prunable: true},
	{Service: "ec2", Key: "vpn_connections", Type: TypeVPNConnection, Primary: true, Prunable: true},
	{Service: "ec2", Key: "vpn_gateways", Type: TypeVPNGateway, Prunable: true},
	{Service: "kms", Key: "keys", Type: TypeKMSKey, Primary: true, NoFilters: true},
	{Service: "secretsmanager", Key: "secrets", Type: TypeSecret, Primary: true},
	{Service: "ssm", Key: "parameters", Type: TypeParameter, Primary: true},
}

// byType indexes Registry by record type.
var byType = func() map[string]ResourceType {
	out := make(map[string]ResourceType, len(Registry))
	for _, t := range Registry {
		out[t.Type] = t
	}
	return out
}()

// Lookup returns the registry entry for a record type.
func Lookup(typ string) (ResourceType, bool) {
	t, ok := byType[typ]
	return t, ok
}

// Validate checks the config's resource keys against the registry and each
// query against its type's rules. Run it before any AWS call, so a typo fails
// before the slow part.
func Validate(r *config.Resources) error {
	known := map[string]map[string]ResourceType{}
	for _, t := range Registry {
		if known[t.Service] == nil {
			known[t.Service] = map[string]ResourceType{}
		}
		known[t.Service][t.Key] = t
	}
	for _, service := range slices.Sorted(maps.Keys(r.Services)) {
		types, ok := known[service]
		if !ok {
			return fmt.Errorf("resources.%s: unknown service; supported: %s",
				service, strings.Join(slices.Sorted(maps.Keys(known)), ", "))
		}
		for _, key := range slices.Sorted(maps.Keys(r.Services[service])) {
			t, ok := types[key]
			if !ok {
				return fmt.Errorf("resources.%s.%s: unknown resource type; supported: %s",
					service, key, strings.Join(slices.Sorted(maps.Keys(types)), ", "))
			}
			if q := r.Services[service][key]; t.NoFilters && q != nil && len(q.Filters) > 0 {
				return fmt.Errorf("resources.%s: filters are not supported; its list calls take none, so use ids",
					t.Name())
			}
		}
	}
	return nil
}
