package azure

import (
	"slices"
	"strings"

	"diagram/internal/config"
	"diagram/internal/inventory"
)

// kind is how Finish applies the config to one resource type.
type kind struct {
	query func(*config.AzureResources) *config.AzureResourceQuery
	// prunable types are dropped when unconnected and the config asks for
	// that. Primary types are never pruned: they are what the config asks
	// to draw.
	prunable bool
}

func network(pick func(*config.AzureNetworkResources) *config.AzureResourceQuery) func(
	*config.AzureResources,
) *config.AzureResourceQuery {
	return config.AzureQuery(config.AzureNetwork, pick)
}

func compute(pick func(*config.AzureComputeResources) *config.AzureResourceQuery) func(
	*config.AzureResources,
) *config.AzureResourceQuery {
	return config.AzureQuery(config.AzureCompute, pick)
}

// kinds lists the resource types the config can hide or prune. Any other
// type, such as one read through include_all, is always kept.
var kinds = map[string]kind{
	TypeVirtualNetwork: {network(func(n *config.AzureNetworkResources) *config.AzureResourceQuery {
		return n.VirtualNetworks
	}), true},
	TypeSubnet: {network(func(n *config.AzureNetworkResources) *config.AzureResourceQuery { return n.Subnets }), true},
	TypeNetworkSecurityGroup: {network(func(n *config.AzureNetworkResources) *config.AzureResourceQuery {
		return n.NetworkSecurityGroups
	}), true},
	TypeFirewall: {network(func(n *config.AzureNetworkResources) *config.AzureResourceQuery {
		return n.Firewalls
	}), false},
	TypePrivateEndpoint: {network(func(n *config.AzureNetworkResources) *config.AzureResourceQuery {
		return n.PrivateEndpoints
	}), false},
	TypeVirtualMachine: {compute(func(c *config.AzureComputeResources) *config.AzureResourceQuery {
		return c.VirtualMachines
	}), false},
	TypeScaleSet: {compute(func(c *config.AzureComputeResources) *config.AzureResourceQuery {
		return c.VirtualMachineScaleSets
	}), false},
	TypeDisk: {compute(func(c *config.AzureComputeResources) *config.AzureResourceQuery { return c.Disks }), false},
	TypeKeyVault: {
		config.AzureQuery(config.AzureKeyVault, func(k *config.AzureKeyVaultResources) *config.AzureResourceQuery {
			return k.Vaults
		}),
		false,
	},
}

// Finish completes the records a source read, region by region. It copies
// what a virtual machine's network interfaces and public IPs hold onto the
// machine and drops them, makes each resource with network interfaces a
// member of its subnets' network security groups, sets each record's
// Parent, and applies the config's hide and show_unconnected_resources
// settings. The model reads only what Finish sets.
func Finish(cfg *config.Config, regions []*inventory.Region) {
	_, sub := cfg.Subscription()
	for _, r := range regions {
		resolveInterfaces(r)
		resolveParents(r)
		connect(r)
		applyConfig(r.Records, &sub.Resources, cfg.Options.ShowUnconnectedResources)
	}
}

// resolveInterfaces copies each virtual machine's network interfaces onto
// it: their subnets, network security groups, primary private IP, and
// public IPs, which also make it public. A firewall's public IPs are copied
// the same way. Network interface and public IP records are then dropped:
// they are how Azure wires a resource, not resources the diagram draws.
//
// Every resource with a network interface is then a member of its subnets'
// network security groups, since a subnet's group filters its traffic as
// its interface's does.
func resolveInterfaces(r *inventory.Region) {
	byID := map[string]*inventory.Record{}
	for i := range r.Records {
		byID[r.Records[i].ID] = &r.Records[i]
	}
	publicIPs := func(refs []inventory.Ref) []string {
		var ips []string
		for _, ref := range refs {
			if p := byID[ref.ID]; ref.Relation == RelPublicIP && p != nil && p.Attrs[AttrIP] != "" {
				ips = append(ips, p.Attrs[AttrIP])
			}
		}
		return ips
	}

	for i := range r.Records {
		rec := &r.Records[i]
		var refs []inventory.Ref
		for _, ref := range rec.Refs {
			if ref.Relation != RelNetworkInterface {
				refs = append(refs, ref)
				continue
			}
			nic := byID[ref.ID]
			if nic == nil {
				continue
			}
			refs = append(refs, nic.Refs...)
			if rec.Attrs[AttrPrivateIP] == "" && nic.Attrs[AttrPrivateIP] != "" {
				setAttr(rec, AttrPrivateIP, nic.Attrs[AttrPrivateIP])
			}
		}
		rec.Refs = refs
		if ips := publicIPs(rec.Refs); len(ips) > 0 {
			setAttr(rec, AttrPublicIPs, strings.Join(ips, ","))
			rec.Tier = inventory.TierPublic
		}
	}

	r.Records = slices.DeleteFunc(r.Records, func(rec inventory.Record) bool {
		return rec.Type == TypeNetworkInterface || rec.Type == TypePublicIP
	})

	subnetNSG := map[string]string{}
	for _, rec := range r.Records {
		for _, ref := range rec.Refs {
			if ref.Relation == RelSubnetNSG {
				subnetNSG[rec.ID] = ref.ID
			}
		}
	}
	for i := range r.Records {
		rec := &r.Records[i]
		if rec.Type != TypeVirtualMachine && rec.Type != TypeScaleSet {
			continue
		}
		for _, ref := range rec.Refs {
			if nsg := subnetNSG[ref.ID]; ref.Relation == inventory.RelContainedIn && nsg != "" {
				rec.Refs = addMembership(rec.Refs, nsg)
			}
		}
	}
}

// addMembership adds a membership in group unless refs already hold it.
func addMembership(refs []inventory.Ref, group string) []inventory.Ref {
	m := ref(inventory.RelMemberOf, TypeNetworkSecurityGroup, group)
	if slices.Contains(refs, m) {
		return refs
	}
	return append(refs, m)
}

func setAttr(rec *inventory.Record, key, value string) {
	if rec.Attrs == nil {
		rec.Attrs = map[string]string{}
	}
	rec.Attrs[key] = value
}

// resolveParents sets each record's Parent: the first subnet it lists that
// the region holds, else the virtual network of the first subnet it lists
// when the region holds that, else none. A subnet's parent is its network.
// An attached disk sits with its virtual machine. Every reference but
// memberships is then removed.
func resolveParents(r *inventory.Region) {
	present := map[string]bool{}
	for _, rec := range r.Records {
		present[rec.ID] = true
	}
	for i := range r.Records {
		rec := &r.Records[i]
		rec.Parent = ""
		var containers []string
		for _, ref := range rec.Refs {
			if ref.Relation == inventory.RelContainedIn {
				containers = append(containers, ref.ID)
			}
		}
		if i := slices.IndexFunc(containers, func(c string) bool { return present[c] }); i >= 0 {
			rec.Parent = containers[i]
		} else if len(containers) > 0 && present[vnetOf(containers[0])] {
			rec.Parent = vnetOf(containers[0])
		}
	}

	parentOf := map[string]string{}
	for _, rec := range r.Records {
		parentOf[rec.ID] = rec.Parent
	}
	for i := range r.Records {
		rec := &r.Records[i]
		for _, ref := range rec.Refs {
			if ref.Relation == RelManagedBy && present[ref.ID] {
				rec.Parent = parentOf[ref.ID]
			}
		}
		rec.Refs = slices.DeleteFunc(rec.Refs, func(ref inventory.Ref) bool {
			return ref.Relation != inventory.RelMemberOf
		})
	}
}

// connect says what keeps each record when unconnected resources are pruned.
// A network or subnet is kept by what it holds, which the model reads from
// Parent; a network security group is kept by its members.
func connect(r *inventory.Region) {
	members := map[string][]string{}
	for _, rec := range r.Records {
		for _, ref := range rec.Refs {
			members[ref.ID] = append(members[ref.ID], rec.ID)
		}
	}
	for i := range r.Records {
		if rec := &r.Records[i]; rec.Type == TypeNetworkSecurityGroup {
			rec.KeepIf = members[rec.ID]
		}
	}
}

// applyConfig sets each record's hide flags from its type's query, and marks
// it prunable when its type can be pruned and its show_unconnected_resources
// setting is false: the type's own value, else the global option, else true.
// Listed IDs mean "draw these", so a type queried by ID is never pruned.
func applyConfig(records []inventory.Record, res *config.AzureResources, global *bool) {
	show := func(q *config.AzureResourceQuery) bool {
		if q != nil && len(q.IDs) > 0 {
			return true
		}
		if q != nil && q.ShowUnconnectedResources != nil {
			return *q.ShowUnconnectedResources
		}
		if global != nil {
			return *global
		}
		return true
	}
	for i := range records {
		rec := &records[i]
		k, ok := kinds[rec.Type]
		if !ok {
			continue
		}
		q := k.query(res)
		if q != nil {
			rec.Hidden, rec.HideChildren = q.Hide, q.HideChildren
		}
		rec.Prunable = k.prunable && !show(q)
	}
}
