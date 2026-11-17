package azure_steampipe

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v8"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v9"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/platform/azure"
	"diagram/internal/steampipe"
)

// baseColumns are the columns every table reads: the ID, the name, and the
// resource group the config can select by.
var baseColumns = []string{"id", "name", "resource_group"}

// newTable returns a table read by its ARM ID, which Azure compares
// case-insensitively.
func newTable(name string, columns ...string) steampipe.Table {
	return steampipe.Table{Name: name, IDColumn: "id", Columns: slices.Concat(baseColumns, columns), FoldIDCase: true}
}

func networkQuery(pick func(*config.AzureNetworkResources) *config.AzureResourceQuery) func(
	*config.AzureResources,
) *config.AzureResourceQuery {
	return config.AzureQuery(config.AzureNetwork, pick)
}

func computeQuery(pick func(*config.AzureComputeResources) *config.AzureResourceQuery) func(
	*config.AzureResources,
) *config.AzureResourceQuery {
	return config.AzureQuery(config.AzureCompute, pick)
}

// tables lists every table the reader serves, in the order the model adds
// them. The jsonb columns hold ARM JSON, which the SDK types decode.
var tables = []table{
	{
		Table:    newTable("azure_virtual_network", "address_prefixes", "subnets"),
		resource: "network.virtual_networks",
		query: networkQuery(func(n *config.AzureNetworkResources) *config.AzureResourceQuery {
			return n.VirtualNetworks
		}),
		always: true,
		record: virtualNetworkRecords,
	},
	{
		Table:    newTable("azure_network_security_group", "security_rules"),
		resource: "network.network_security_groups",
		query: networkQuery(func(n *config.AzureNetworkResources) *config.AzureResourceQuery {
			return n.NetworkSecurityGroups
		}),
		always: true,
		record: networkSecurityGroupRecord,
	},
	{
		Table:    newTable("azure_network_interface", "ip_configurations", "network_security_group_id"),
		resource: "network.network_interfaces",
		query: networkQuery(func(n *config.AzureNetworkResources) *config.AzureResourceQuery {
			return n.NetworkInterfaces
		}),
		always: true,
		record: networkInterfaceRecord,
	},
	{
		Table:    newTable("azure_public_ip", "ip_address"),
		resource: "network.public_ips",
		query:    networkQuery(func(n *config.AzureNetworkResources) *config.AzureResourceQuery { return n.PublicIPs }),
		always:   true,
		record:   publicIPRecord,
	},
	{
		Table:    newTable("azure_compute_virtual_machine", "size", "os_type", "network_interfaces"),
		resource: "compute.virtual_machines",
		query: computeQuery(func(c *config.AzureComputeResources) *config.AzureResourceQuery {
			return c.VirtualMachines
		}),
		record: virtualMachineRecord,
	},
	{
		Table: newTable(
			"azure_compute_virtual_machine_scale_set", "sku_name", "sku_capacity", "virtual_machine_network_profile",
		),
		resource: "compute.virtual_machine_scale_sets",
		query: computeQuery(func(c *config.AzureComputeResources) *config.AzureResourceQuery {
			return c.VirtualMachineScaleSets
		}),
		record: scaleSetRecord,
	},
	{
		Table:    newTable("azure_compute_disk", "managed_by", "sku_name", "disk_size_gb"),
		resource: "compute.disks",
		query:    computeQuery(func(c *config.AzureComputeResources) *config.AzureResourceQuery { return c.Disks }),
		record:   diskRecord,
	},
	{
		Table:    newTable("azure_firewall", "sku_tier", "ip_configurations"),
		resource: "network.firewalls",
		query:    networkQuery(func(n *config.AzureNetworkResources) *config.AzureResourceQuery { return n.Firewalls }),
		record:   firewallRecord,
	},
	{
		Table: newTable(
			"azure_private_endpoint", "subnet", "private_link_service_connections",
			"manual_private_link_service_connections",
		),
		resource: "network.private_endpoints",
		query: networkQuery(func(n *config.AzureNetworkResources) *config.AzureResourceQuery {
			return n.PrivateEndpoints
		}),
		record: privateEndpointRecord,
	},
	{
		Table:    newTable("azure_key_vault", "vault_uri"),
		resource: "keyvault.vaults",
		query: config.AzureQuery(
			config.AzureKeyVault,
			func(k *config.AzureKeyVaultResources) *config.AzureResourceQuery {
				return k.Vaults
			},
		),
		record: keyVaultRecord,
	},
}

// text returns a column's value, or nil for NULL or an empty value, as the
// SDK leaves an absent field.
func text(row steampipe.Row, column string) *string {
	if v := steampipe.Text(row, column); v != "" {
		return &v
	}
	return nil
}

// one wraps a single record.
func one(rec inventory.Record) []inventory.Record {
	return []inventory.Record{rec}
}

// decodeErr names the resource a column failed to decode for.
func decodeErr(row steampipe.Row, err error) error {
	return fmt.Errorf("%s: %w", steampipe.Text(row, "id"), err)
}

// virtualNetworkRecords reads an azure_virtual_network row: the network and
// the subnets it holds. Steampipe has no subnet table.
func virtualNetworkRecords(row steampipe.Row) ([]inventory.Record, error) {
	props := &armnetwork.VirtualNetworkPropertiesFormat{AddressSpace: &armnetwork.AddressSpace{}}
	if err := steampipe.Decode(row, "address_prefixes", &props.AddressSpace.AddressPrefixes); err != nil {
		return nil, decodeErr(row, err)
	}
	if err := steampipe.Decode(row, "subnets", &props.Subnets); err != nil {
		return nil, decodeErr(row, err)
	}
	v := armnetwork.VirtualNetwork{ID: text(row, "id"), Name: text(row, "name"), Properties: props}
	return append(one(azure.VirtualNetwork(v)), azure.Subnets(v)...), nil
}

// networkSecurityGroupRecord reads an azure_network_security_group row. Only
// its own rules are read, not Azure's defaults.
func networkSecurityGroupRecord(row steampipe.Row) ([]inventory.Record, error) {
	props := &armnetwork.SecurityGroupPropertiesFormat{}
	if err := steampipe.Decode(row, "security_rules", &props.SecurityRules); err != nil {
		return nil, decodeErr(row, err)
	}
	g := armnetwork.SecurityGroup{ID: text(row, "id"), Name: text(row, "name"), Properties: props}
	return one(azure.NetworkSecurityGroup(g)), nil
}

// networkInterfaceRecord reads an azure_network_interface row.
func networkInterfaceRecord(row steampipe.Row) ([]inventory.Record, error) {
	props := &armnetwork.InterfacePropertiesFormat{}
	if err := steampipe.Decode(row, "ip_configurations", &props.IPConfigurations); err != nil {
		return nil, decodeErr(row, err)
	}
	if nsg := text(row, "network_security_group_id"); nsg != nil {
		props.NetworkSecurityGroup = &armnetwork.SecurityGroup{ID: nsg}
	}
	n := armnetwork.Interface{ID: text(row, "id"), Name: text(row, "name"), Properties: props}
	return one(azure.NetworkInterface(n)), nil
}

// publicIPRecord reads an azure_public_ip row.
func publicIPRecord(row steampipe.Row) ([]inventory.Record, error) {
	return one(azure.PublicIP(armnetwork.PublicIPAddress{
		ID: text(row, "id"), Name: text(row, "name"),
		Properties: &armnetwork.PublicIPAddressPropertiesFormat{IPAddress: text(row, "ip_address")},
	})), nil
}

// virtualMachineRecord reads an azure_compute_virtual_machine row.
func virtualMachineRecord(row steampipe.Row) ([]inventory.Record, error) {
	size := armcompute.VirtualMachineSizeTypes(steampipe.Text(row, "size"))
	osType := armcompute.OperatingSystemTypes(steampipe.Text(row, "os_type"))
	props := &armcompute.VirtualMachineProperties{
		HardwareProfile: &armcompute.HardwareProfile{VMSize: &size},
		StorageProfile:  &armcompute.StorageProfile{OSDisk: &armcompute.OSDisk{OSType: &osType}},
		NetworkProfile:  &armcompute.NetworkProfile{},
	}
	if err := steampipe.Decode(row, "network_interfaces", &props.NetworkProfile.NetworkInterfaces); err != nil {
		return nil, decodeErr(row, err)
	}
	vm := armcompute.VirtualMachine{ID: text(row, "id"), Name: text(row, "name"), Properties: props}
	return one(azure.VirtualMachine(vm)), nil
}

// scaleSetRecord reads an azure_compute_virtual_machine_scale_set row.
func scaleSetRecord(row steampipe.Row) ([]inventory.Record, error) {
	s := armcompute.VirtualMachineScaleSet{
		ID: text(row, "id"), Name: text(row, "name"),
		SKU: &armcompute.SKU{Name: text(row, "sku_name")},
		Properties: &armcompute.VirtualMachineScaleSetProperties{
			VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{},
		},
	}
	if c, err := strconv.ParseInt(steampipe.Text(row, "sku_capacity"), 10, 64); err == nil {
		s.SKU.Capacity = &c
	}
	profile := s.Properties.VirtualMachineProfile
	if err := steampipe.Decode(row, "virtual_machine_network_profile", &profile.NetworkProfile); err != nil {
		return nil, decodeErr(row, err)
	}
	return one(azure.ScaleSet(s)), nil
}

// diskRecord reads an azure_compute_disk row.
func diskRecord(row steampipe.Row) ([]inventory.Record, error) {
	sku := armcompute.DiskStorageAccountTypes(steampipe.Text(row, "sku_name"))
	d := armcompute.Disk{
		ID: text(row, "id"), Name: text(row, "name"), ManagedBy: text(row, "managed_by"),
		SKU: &armcompute.DiskSKU{Name: &sku}, Properties: &armcompute.DiskProperties{},
	}
	if gb, err := strconv.ParseInt(steampipe.Text(row, "disk_size_gb"), 10, 32); err == nil {
		size := int32(gb)
		d.Properties.DiskSizeGB = &size
	}
	return one(azure.Disk(d)), nil
}

// firewallIPConfiguration is how Steampipe stores a firewall's IP
// configurations: the ARM properties flattened, without the "properties"
// level the SDK type expects.
type firewallIPConfiguration struct {
	PrivateIPAddress *string                 `json:"privateIPAddress"`
	PublicIPAddress  *armnetwork.SubResource `json:"publicIPAddress"`
	Subnet           *armnetwork.SubResource `json:"subnet"`
}

// firewallRecord reads an azure_firewall row.
func firewallRecord(row steampipe.Row) ([]inventory.Record, error) {
	var configs []firewallIPConfiguration
	if err := steampipe.Decode(row, "ip_configurations", &configs); err != nil {
		return nil, decodeErr(row, err)
	}
	tier := armnetwork.AzureFirewallSKUTier(steampipe.Text(row, "sku_tier"))
	props := &armnetwork.AzureFirewallPropertiesFormat{SKU: &armnetwork.AzureFirewallSKU{Tier: &tier}}
	for _, c := range configs {
		props.IPConfigurations = append(props.IPConfigurations, &armnetwork.AzureFirewallIPConfiguration{
			Properties: &armnetwork.AzureFirewallIPConfigurationPropertiesFormat{
				PrivateIPAddress: c.PrivateIPAddress, PublicIPAddress: c.PublicIPAddress, Subnet: c.Subnet,
			},
		})
	}
	f := armnetwork.AzureFirewall{ID: text(row, "id"), Name: text(row, "name"), Properties: props}
	return one(azure.Firewall(f)), nil
}

// privateEndpointRecord reads an azure_private_endpoint row.
func privateEndpointRecord(row steampipe.Row) ([]inventory.Record, error) {
	props := &armnetwork.PrivateEndpointProperties{}
	for column, dst := range map[string]any{
		"subnet":                                  &props.Subnet,
		"private_link_service_connections":        &props.PrivateLinkServiceConnections,
		"manual_private_link_service_connections": &props.ManualPrivateLinkServiceConnections,
	} {
		if err := steampipe.Decode(row, column, dst); err != nil {
			return nil, decodeErr(row, err)
		}
	}
	p := armnetwork.PrivateEndpoint{ID: text(row, "id"), Name: text(row, "name"), Properties: props}
	return one(azure.PrivateEndpoint(p)), nil
}

// keyVaultRecord reads an azure_key_vault row: its ID, name, and URI only.
func keyVaultRecord(row steampipe.Row) ([]inventory.Record, error) {
	return one(azure.KeyVault(
		steampipe.Text(row, "id"), steampipe.Text(row, "name"), steampipe.Text(row, "vault_uri"),
	)), nil
}
