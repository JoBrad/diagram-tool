package azure

import (
	"slices"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v8"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v9"

	"diagram/internal/inventory"
)

// The record builders below each turn one SDK resource into a record. A
// source leaves out resources that no longer exist, so none of them filter
// by state.

// VirtualNetwork builds a virtual network's record. Its subnets are records
// of their own; see Subnets.
func VirtualNetwork(v armnetwork.VirtualNetwork) inventory.Record {
	rec := record(id(v.ID), TypeVirtualNetwork, val(v.Name))
	rec.Attrs = map[string]string{AttrResourceGroup: resourceGroup(rec.ID)}
	if v.Properties != nil && v.Properties.AddressSpace != nil {
		rec.Attrs[AttrCIDR] = strings.Join(values(v.Properties.AddressSpace.AddressPrefixes), ",")
	}
	return rec
}

// Subnets builds the records of a virtual network's subnets, which Azure
// returns inside the network. A subnet spans every zone of its region, so it
// has no zone. Azure has no subnet setting that makes it public, so its tier
// is left unknown.
func Subnets(v armnetwork.VirtualNetwork) []inventory.Record {
	if v.Properties == nil {
		return nil
	}
	vnet := id(v.ID)
	var out []inventory.Record
	for _, s := range v.Properties.Subnets {
		if s == nil {
			continue
		}
		rec := record(id(s.ID), TypeSubnet, val(s.Name))
		rec.Refs = []inventory.Ref{ref(inventory.RelContainedIn, TypeVirtualNetwork, vnet)}
		rec.Attrs = map[string]string{AttrResourceGroup: resourceGroup(rec.ID)}
		if p := s.Properties; p != nil {
			rec.Attrs[AttrCIDR] = strings.Join(nonEmpty(p.AddressPrefix, p.AddressPrefixes), ",")
			if p.NetworkSecurityGroup != nil && p.NetworkSecurityGroup.ID != nil {
				nsg := id(p.NetworkSecurityGroup.ID)
				rec.Attrs[AttrNSG] = nsg
				rec.Refs = append(rec.Refs, ref(RelSubnetNSG, TypeNetworkSecurityGroup, nsg))
			}
			if p.NatGateway != nil && p.NatGateway.ID != nil {
				rec.Attrs[AttrNATGateway] = id(p.NatGateway.ID)
			}
		}
		out = append(out, rec)
	}
	return out
}

// NetworkSecurityGroup builds a network security group's record, with its
// own rules. Azure's default rules, the same in every group, are left out.
func NetworkSecurityGroup(g armnetwork.SecurityGroup) inventory.Record {
	rec := record(id(g.ID), TypeNetworkSecurityGroup, val(g.Name))
	rec.Attrs = map[string]string{AttrResourceGroup: resourceGroup(rec.ID)}
	if g.Properties != nil {
		rec.Rules = SecurityRules(g.Properties.SecurityRules)
	}
	return rec
}

// NetworkInterface builds a network interface's record: its subnets, primary
// IP configuration first, its public IPs, its network security group, and
// its primary private IP. Finish copies these onto the virtual machine that
// uses the interface and drops the interface.
func NetworkInterface(n armnetwork.Interface) inventory.Record {
	rec := record(id(n.ID), TypeNetworkInterface, val(n.Name))
	rec.Attrs = map[string]string{}
	if n.Properties == nil {
		return rec
	}
	configs := primaryFirst(n.Properties.IPConfigurations, func(c *armnetwork.InterfaceIPConfiguration) bool {
		return c.Properties != nil && val(c.Properties.Primary)
	})
	for _, c := range configs {
		p := c.Properties
		if p == nil {
			continue
		}
		if rec.Attrs[AttrPrivateIP] == "" {
			rec.Attrs[AttrPrivateIP] = val(p.PrivateIPAddress)
		}
		if p.Subnet != nil && p.Subnet.ID != nil {
			rec.Refs = append(rec.Refs, ref(inventory.RelContainedIn, TypeSubnet, id(p.Subnet.ID)))
		}
		if p.PublicIPAddress != nil && p.PublicIPAddress.ID != nil {
			rec.Refs = append(rec.Refs, ref(RelPublicIP, TypePublicIP, id(p.PublicIPAddress.ID)))
		}
	}
	if g := n.Properties.NetworkSecurityGroup; g != nil && g.ID != nil {
		rec.Refs = append(rec.Refs, ref(inventory.RelMemberOf, TypeNetworkSecurityGroup, id(g.ID)))
	}
	return rec
}

// PublicIP builds a public IP address's record. Finish copies its address
// onto the resource that uses it and drops the record.
func PublicIP(p armnetwork.PublicIPAddress) inventory.Record {
	rec := record(id(p.ID), TypePublicIP, val(p.Name))
	rec.Attrs = map[string]string{}
	if p.Properties != nil {
		rec.Attrs[AttrIP] = val(p.Properties.IPAddress)
	}
	return rec
}

// VirtualMachine builds a virtual machine's record, referring to its network
// interfaces, primary first. Finish places it from them.
func VirtualMachine(vm armcompute.VirtualMachine) inventory.Record {
	rec := record(id(vm.ID), TypeVirtualMachine, val(vm.Name))
	rec.Attrs = map[string]string{AttrResourceGroup: resourceGroup(rec.ID)}
	p := vm.Properties
	if p == nil {
		return rec
	}
	if p.HardwareProfile != nil {
		rec.Attrs[AttrSize] = string(val(p.HardwareProfile.VMSize))
	}
	if p.StorageProfile != nil && p.StorageProfile.OSDisk != nil {
		rec.Attrs[AttrOSType] = string(val(p.StorageProfile.OSDisk.OSType))
	}
	if p.NetworkProfile != nil {
		nics := primaryFirst(p.NetworkProfile.NetworkInterfaces, func(n *armcompute.NetworkInterfaceReference) bool {
			return n.Properties != nil && val(n.Properties.Primary)
		})
		for _, n := range nics {
			if n.ID != nil {
				rec.Refs = append(rec.Refs, ref(RelNetworkInterface, TypeNetworkInterface, id(n.ID)))
			}
		}
	}
	return rec
}

// ScaleSet builds a virtual machine scale set's record, placed in the
// subnets its network profile names and a member of its network security
// groups. Its instances are not drawn.
func ScaleSet(s armcompute.VirtualMachineScaleSet) inventory.Record {
	rec := record(id(s.ID), TypeScaleSet, val(s.Name))
	rec.Attrs = map[string]string{AttrResourceGroup: resourceGroup(rec.ID)}
	if s.SKU != nil {
		rec.Attrs[AttrSKU] = val(s.SKU.Name)
		if s.SKU.Capacity != nil {
			rec.Attrs[AttrCapacity] = strconv.FormatInt(*s.SKU.Capacity, 10)
		}
	}
	if s.Properties == nil || s.Properties.VirtualMachineProfile == nil ||
		s.Properties.VirtualMachineProfile.NetworkProfile == nil {
		return rec
	}
	configs := primaryFirst(
		s.Properties.VirtualMachineProfile.NetworkProfile.NetworkInterfaceConfigurations,
		func(c *armcompute.VirtualMachineScaleSetNetworkConfiguration) bool {
			return c.Properties != nil && val(c.Properties.Primary)
		},
	)
	for _, c := range configs {
		if c.Properties == nil {
			continue
		}
		for _, ip := range c.Properties.IPConfigurations {
			if ip != nil && ip.Properties != nil && ip.Properties.Subnet != nil && ip.Properties.Subnet.ID != nil {
				rec.Refs = append(rec.Refs, ref(inventory.RelContainedIn, TypeSubnet, id(ip.Properties.Subnet.ID)))
			}
		}
		if g := c.Properties.NetworkSecurityGroup; g != nil && g.ID != nil {
			rec.Refs = append(rec.Refs, ref(inventory.RelMemberOf, TypeNetworkSecurityGroup, id(g.ID)))
		}
	}
	return rec
}

// Firewall builds an Azure Firewall's record, placed in its subnet (the
// network's AzureFirewallSubnet), with its private IP and public IPs.
func Firewall(f armnetwork.AzureFirewall) inventory.Record {
	rec := record(id(f.ID), TypeFirewall, val(f.Name))
	rec.Attrs = map[string]string{AttrResourceGroup: resourceGroup(rec.ID)}
	if f.Properties == nil {
		return rec
	}
	if f.Properties.SKU != nil {
		rec.Attrs[AttrSKU] = string(val(f.Properties.SKU.Tier))
	}
	for _, c := range f.Properties.IPConfigurations {
		if c == nil || c.Properties == nil {
			continue
		}
		p := c.Properties
		if rec.Attrs[AttrPrivateIP] == "" {
			rec.Attrs[AttrPrivateIP] = val(p.PrivateIPAddress)
		}
		if p.Subnet != nil && p.Subnet.ID != nil {
			rec.Refs = append(rec.Refs, ref(inventory.RelContainedIn, TypeSubnet, id(p.Subnet.ID)))
		}
		if p.PublicIPAddress != nil && p.PublicIPAddress.ID != nil {
			rec.Refs = append(rec.Refs, ref(RelPublicIP, TypePublicIP, id(p.PublicIPAddress.ID)))
		}
	}
	return rec
}

// PrivateEndpoint builds a private endpoint's record, placed in its subnet,
// with the resources it connects to.
func PrivateEndpoint(p armnetwork.PrivateEndpoint) inventory.Record {
	rec := record(id(p.ID), TypePrivateEndpoint, val(p.Name))
	rec.Attrs = map[string]string{AttrResourceGroup: resourceGroup(rec.ID)}
	if p.Properties == nil {
		return rec
	}
	if s := p.Properties.Subnet; s != nil && s.ID != nil {
		rec.Refs = append(rec.Refs, ref(inventory.RelContainedIn, TypeSubnet, id(s.ID)))
	}
	var targets []string
	conns := slices.Concat(p.Properties.PrivateLinkServiceConnections, p.Properties.ManualPrivateLinkServiceConnections)
	for _, c := range conns {
		if c != nil && c.Properties != nil && c.Properties.PrivateLinkServiceID != nil {
			targets = append(targets, id(c.Properties.PrivateLinkServiceID))
		}
	}
	rec.Attrs[AttrLinkTargets] = strings.Join(targets, ",")
	return rec
}

// Disk builds a managed disk's record. Finish places an attached disk with
// the virtual machine it is attached to.
func Disk(d armcompute.Disk) inventory.Record {
	rec := record(id(d.ID), TypeDisk, val(d.Name))
	rec.Attrs = map[string]string{AttrResourceGroup: resourceGroup(rec.ID)}
	if d.SKU != nil {
		rec.Attrs[AttrSKU] = string(val(d.SKU.Name))
	}
	if d.Properties != nil && d.Properties.DiskSizeGB != nil {
		rec.Attrs[AttrDiskSizeGB] = strconv.Itoa(int(*d.Properties.DiskSizeGB))
	}
	if d.ManagedBy != nil && *d.ManagedBy != "" {
		rec.Refs = []inventory.Ref{ref(RelManagedBy, TypeVirtualMachine, id(d.ManagedBy))}
	}
	return rec
}

// KeyVault builds a key vault's record from its ID, name, and URI only. It
// takes plain strings so nothing from inside the vault can reach a record.
func KeyVault(vaultID, name, uri string) inventory.Record {
	rec := record(strings.ToLower(vaultID), TypeKeyVault, name)
	rec.Attrs = map[string]string{AttrResourceGroup: resourceGroup(rec.ID), AttrVaultURI: uri}
	return rec
}

// Generic builds the record of a resource type with no builder of its own,
// such as one read through include_all: its ID, name, and the subnets it
// sits in, when known. Finish keeps the first subnet that is drawn.
func Generic(rt, resourceID, name string, subnets []string) inventory.Record {
	rec := record(strings.ToLower(resourceID), rt, name)
	rec.Attrs = map[string]string{AttrResourceGroup: resourceGroup(rec.ID)}
	for _, s := range subnets {
		rec.Refs = append(rec.Refs, ref(inventory.RelContainedIn, TypeSubnet, strings.ToLower(s)))
	}
	return rec
}

// record starts an Azure record. A resource without a name is named by its
// ID.
func record(id, rt, name string) inventory.Record {
	if name == "" {
		name = id
	}
	return inventory.Record{ID: id, Type: rt, Platform: Platform, Name: name}
}

func ref(rel inventory.Relation, resourceType, id string) inventory.Ref {
	return inventory.Ref{Relation: rel, Type: resourceType, ID: id}
}

// id returns a resource ID lowercased, since Azure compares IDs
// case-insensitively and returns them in mixed case.
func id(s *string) string {
	return strings.ToLower(val(s))
}

// resourceGroup returns the resource group named in a lowercased resource
// ID, or "".
func resourceGroup(resourceID string) string {
	_, rest, ok := strings.Cut(resourceID, "/resourcegroups/")
	if !ok {
		return ""
	}
	rg, _, _ := strings.Cut(rest, "/")
	return rg
}

// vnetOf returns the virtual network a subnet ID belongs to, or "" for an ID
// that is not a subnet's.
func vnetOf(subnet string) string {
	vnet, _, ok := strings.Cut(subnet, "/subnets/")
	if !ok {
		return ""
	}
	return vnet
}

// values returns the set strings of a list of pointers.
func values(ss []*string) []string {
	return nonEmpty(nil, ss)
}

// primaryFirst returns items without nils, the primary one first and the
// rest in order.
func primaryFirst[T any](items []*T, primary func(*T) bool) []*T {
	var first, rest []*T
	for _, it := range items {
		switch {
		case it == nil:
		case primary(it) && len(first) == 0:
			first = append(first, it)
		default:
			rest = append(rest, it)
		}
	}
	return append(first, rest...)
}

// val dereferences an SDK pointer field, giving the zero value for nil.
func val[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
