// Package azure holds what the tool knows about Azure resources, whichever
// source they were read from: their resource types, how each becomes an
// inventory record, and how they connect, contain one another, and are
// selected.
//
// Sources live in subpackages (azure_steampipe reads Steampipe tables). Each
// decodes what it reads into the Azure SDK's types and hands them to this
// package's record builders. Once a source has read every region, Finish
// resolves what Azure spreads across resources (a virtual machine's subnet,
// IPs, and network security groups live on its network interfaces) and
// applies the config's selection settings, after which the model can draw
// the records without knowing they are Azure resources.
//
// Azure resource IDs are case-insensitive, and Azure itself returns the same
// ID in different cases from different APIs, so every ID in a record is
// lowercased.
package azure

import "strings"

// Platform is the inventory.Record Platform of every Azure record.
const Platform = "azure"

// Resource types: the ARM resource type without the "Microsoft." namespace,
// in the casing Azure's documentation uses. ResourceType maps any casing of
// an ARM type to these.
const (
	TypeVirtualNetwork       = "Network/virtualNetworks"
	TypeSubnet               = "Network/virtualNetworks/subnets"
	TypeNetworkSecurityGroup = "Network/networkSecurityGroups"
	TypeNetworkInterface     = "Network/networkInterfaces"
	TypePublicIP             = "Network/publicIPAddresses"
	TypeFirewall             = "Network/azureFirewalls"
	TypePrivateEndpoint      = "Network/privateEndpoints"
	TypeVirtualMachine       = "Compute/virtualMachines"
	TypeScaleSet             = "Compute/virtualMachineScaleSets"
	TypeDisk                 = "Compute/disks"
	TypeKeyVault             = "KeyVault/vaults"

	// Types read only through include_all.
	TypeManagedCluster      = "ContainerService/managedClusters"
	TypeSQLServer           = "Sql/servers"
	TypeLogAnalytics        = "OperationalInsights/workspaces"
	TypeManagedHSM          = "KeyVault/managedHSMs"
	TypeDiskEncryptionSet   = "Compute/diskEncryptionSets"
	TypeFirewallPolicy      = "Network/firewallPolicies"
	TypeWebAppFirewallRules = "Network/ApplicationGatewayWebApplicationFirewallPolicies"
)

// knownTypes maps each type above, lowercased, to its documented casing.
var knownTypes = func() map[string]string {
	out := map[string]string{}
	for _, t := range []string{
		TypeVirtualNetwork, TypeSubnet, TypeNetworkSecurityGroup, TypeNetworkInterface, TypePublicIP,
		TypeFirewall, TypePrivateEndpoint, TypeVirtualMachine, TypeScaleSet, TypeDisk, TypeKeyVault,
		TypeManagedCluster, TypeSQLServer, TypeLogAnalytics, TypeManagedHSM, TypeDiskEncryptionSet,
		TypeFirewallPolicy, TypeWebAppFirewallRules,
	} {
		out[strings.ToLower(t)] = t
	}
	return out
}()

// ResourceType returns the resource type for an ARM type in any casing, such
// as "microsoft.network/virtualnetworks" from Resource Graph or Steampipe:
// the known type's documented casing, else the ARM type as given. Either way
// the "Microsoft." namespace is dropped; other publishers' namespaces stay.
func ResourceType(arm string) string {
	if len(arm) > len("microsoft.") && strings.EqualFold(arm[:len("microsoft.")], "microsoft.") {
		arm = arm[len("microsoft."):]
	}
	if t, ok := knownTypes[strings.ToLower(arm)]; ok {
		return t
	}
	return arm
}

// Relations that only Finish reads. Each is resolved and removed before the
// model sees the records.
const (
	// RelNetworkInterface points from a virtual machine at its network
	// interfaces, primary first.
	RelNetworkInterface = "network_interface"
	// RelPublicIP points at a public IP address a resource uses.
	RelPublicIP = "public_ip"
	// RelSubnetNSG points from a subnet at the network security group
	// associated with it.
	RelSubnetNSG = "subnet_network_security_group"
	// RelManagedBy points from a disk at the virtual machine it is attached
	// to.
	RelManagedBy = "managed_by"
)

// Rule target types beyond inventory's. An address prefix that is not an IP
// or CIDR, such as "VirtualNetwork", "Internet", or "*", is a service tag.
const (
	TargetServiceTag  = "service_tag"
	TargetAppSecGroup = "application_security_group"
)

// Record attribute keys, read by renderers.
const (
	// AttrCIDR is a comma-separated list of address prefixes.
	AttrCIDR          = "cidr"
	AttrResourceGroup = "resource_group"
	AttrSize          = "size"
	AttrOSType        = "os_type"
	AttrPrivateIP     = "private_ip"
	// AttrPublicIPs is a comma-separated list.
	AttrPublicIPs  = "public_ips"
	AttrIP         = "ip"
	AttrSKU        = "sku"
	AttrCapacity   = "capacity"
	AttrDiskSizeGB = "disk_size_gb"
	AttrVaultURI   = "vault_uri"
	// AttrLinkTargets is a comma-separated list of the resource IDs a
	// private endpoint connects to.
	AttrLinkTargets = "link_targets"
	// AttrNATGateway is the ID of a subnet's NAT gateway.
	AttrNATGateway = "nat_gateway"
	// AttrNSG is the ID of a subnet's network security group.
	AttrNSG = "network_security_group"
)
