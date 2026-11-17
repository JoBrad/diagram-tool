package azure_steampipe

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/platform/azure"
	"diagram/internal/steampipe"
)

// call is one query the fake source received.
type call struct {
	sql  string
	args []any
}

// fakeSource returns canned rows per table and records every query.
type fakeSource struct {
	rows  map[string][]steampipe.Row
	err   error
	calls []call
}

var fromTable = regexp.MustCompile(`FROM (\w+)`)

func (f *fakeSource) Query(_ context.Context, sql string, args ...any) ([]steampipe.Row, error) {
	f.calls = append(f.calls, call{sql, args})
	if f.err != nil {
		return nil, f.err
	}
	return f.rows[fromTable.FindStringSubmatch(sql)[1]], nil
}

// table returns the query sent for a table, or nil.
func (f *fakeSource) table(name string) *call {
	for i, c := range f.calls {
		if fromTable.FindStringSubmatch(c.sql)[1] == name {
			return &f.calls[i]
		}
	}
	return nil
}

// row builds a steampipe.Row from column/value pairs.
func row(pairs ...string) steampipe.Row {
	r := steampipe.Row{}
	for i := 0; i < len(pairs); i += 2 {
		v := pairs[i+1]
		r[pairs[i]] = &v
	}
	return r
}

// Hand-built IDs, in the mixed case Azure returns them in.
const (
	subID   = "00000000-0000-0000-0000-000000000001"
	rg      = "/subscriptions/" + subID + "/resourceGroups/RG-App/providers/"
	vnetID  = rg + "Microsoft.Network/virtualNetworks/VNet-1"
	subnetA = vnetID + "/subnets/App"
	nsgID   = rg + "Microsoft.Network/networkSecurityGroups/NSG-App"
	nicID   = rg + "Microsoft.Network/networkInterfaces/vm-1-nic"
	pipID   = rg + "Microsoft.Network/publicIPAddresses/vm-1-ip"
	vmID    = rg + "Microsoft.Compute/virtualMachines/VM-1"
)

// base returns the columns every table reads, for one resource in eastus.
func base(id, name string, more ...string) steampipe.Row {
	return row(append([]string{"region", "eastus", "id", id, "name", name, "resource_group", "RG-App"}, more...)...)
}

// testRows are one resource of each table, in the JSON Steampipe stores.
func testRows() map[string][]steampipe.Row {
	return map[string][]steampipe.Row{
		"azure_virtual_network": {
			base(vnetID, "VNet-1", "address_prefixes", `["10.0.0.0/16"]`, "subnets", `[
				{"id": "`+subnetA+`", "name": "App", "properties": {"addressPrefix": "10.0.1.0/24",
					"networkSecurityGroup": {"id": "`+nsgID+`"}}},
				{"id": "`+vnetID+`/subnets/AzureFirewallSubnet", "name": "AzureFirewallSubnet",
					"properties": {"addressPrefix": "10.0.0.0/26"}}]`),
			// A region the config does not name.
			row("region", "westus", "id", "/subscriptions/x/vnet-west", "name", "west"),
		},
		"azure_network_security_group": {
			base(nsgID, "NSG-App", "security_rules", `[{"name": "https", "properties": {"access": "Allow",
				"direction": "Inbound", "priority": 100, "protocol": "Tcp", "sourceAddressPrefix": "Internet",
				"destinationPortRange": "443"}}]`),
		},
		"azure_network_interface": {
			base(nicID, "vm-1-nic", "ip_configurations", `[{"name": "ipconfig1", "properties": {"primary": true,
				"privateIPAddress": "10.0.1.4", "subnet": {"id": "`+strings.ToUpper(subnetA)+`"},
				"publicIPAddress": {"id": "`+pipID+`"}}}]`),
		},
		"azure_public_ip": {base(pipID, "vm-1-ip", "ip_address", "203.0.113.5")},
		"azure_compute_virtual_machine": {
			base(vmID, "VM-1", "size", "Standard_B2s", "os_type", "Linux",
				"network_interfaces", `[{"id": "`+nicID+`", "properties": {"primary": true}}]`),
		},
		"azure_compute_virtual_machine_scale_set": {
			base(rg+"Microsoft.Compute/virtualMachineScaleSets/Pool", "Pool", "sku_name", "Standard_D2s_v5",
				"sku_capacity", "2", "virtual_machine_network_profile", `{"networkInterfaceConfigurations": [
					{"name": "nic", "properties": {"primary": true, "ipConfigurations": [
						{"name": "ip", "properties": {"subnet": {"id": "`+subnetA+`"}}}]}}]}`),
		},
		"azure_compute_disk": {
			base(rg+"Microsoft.Compute/disks/VM-1-os", "VM-1-os", "managed_by", vmID, "sku_name", "Premium_LRS",
				"disk_size_gb", "128"),
		},
		"azure_firewall": {
			base(rg+"Microsoft.Network/azureFirewalls/FW", "FW", "sku_tier", "Standard", "ip_configurations",
				`[{"privateIPAddress": "10.0.0.4", "subnet": {"id": "`+vnetID+`/subnets/AzureFirewallSubnet"}}]`),
		},
		"azure_private_endpoint": {
			base(rg+"Microsoft.Network/privateEndpoints/PE-KV", "PE-KV", "subnet", `{"id": "`+subnetA+`"}`,
				"private_link_service_connections", `[{"name": "kv", "properties":
					{"privateLinkServiceId": "`+rg+`Microsoft.KeyVault/vaults/KV-1"}}]`),
		},
		"azure_key_vault": {
			base(rg+"Microsoft.KeyVault/vaults/KV-1", "KV-1", "vault_uri", "https://kv-1.vault.azure.net/"),
		},
	}
}

func everything() *config.AzureSubscription {
	all := &config.AzureResourceQuery{}
	return &config.AzureSubscription{
		Source: config.SourceSteampipe, SubscriptionID: strings.ToUpper(subID), Regions: []string{"eastus"},
		Resources: config.AzureResources{
			Compute:  &config.AzureComputeResources{VirtualMachines: all, VirtualMachineScaleSets: all, Disks: all},
			Network:  &config.AzureNetworkResources{Firewalls: all, PrivateEndpoints: all},
			KeyVault: &config.AzureKeyVaultResources{Vaults: all},
		},
	}
}

func TestCollect(t *testing.T) {
	src := &fakeSource{rows: testRows()}
	regions, err := Collect(context.Background(), src, everything())
	if err != nil {
		t.Fatal(err)
	}
	if len(regions) != 1 || regions[0].Name != "eastus" {
		t.Fatalf("regions = %+v", regions)
	}
	types := map[string]int{}
	byName := map[string]inventory.Record{}
	for _, r := range regions[0].Records {
		types[r.Type]++
		byName[r.Name] = r
	}
	want := map[string]int{
		azure.TypeVirtualNetwork: 1, azure.TypeSubnet: 2, azure.TypeNetworkSecurityGroup: 1,
		azure.TypeNetworkInterface: 1, azure.TypePublicIP: 1, azure.TypeVirtualMachine: 1, azure.TypeScaleSet: 1,
		azure.TypeDisk: 1, azure.TypeFirewall: 1, azure.TypePrivateEndpoint: 1, azure.TypeKeyVault: 1,
	}
	if !reflect.DeepEqual(types, want) {
		t.Errorf("types = %v, want %v", types, want)
	}

	// The account is matched lowercased, and IDs come out lowercased.
	if c := src.table("azure_virtual_network"); c == nil || c.args[0] != subID {
		t.Errorf("virtual network query = %+v, want the lowercased subscription", c)
	}
	if nic := byName["vm-1-nic"]; nic.Refs[0].ID != strings.ToLower(subnetA) {
		t.Errorf("interface refs = %+v, want the subnet ID lowercased", nic.Refs)
	}
	if nsg := byName["NSG-App"]; len(nsg.Rules) != 1 || nsg.Rules[0].Target != "Internet" {
		t.Errorf("NSG rules = %+v", nsg.Rules)
	}
	if fw := byName["FW"]; fw.Attrs[azure.AttrPrivateIP] != "10.0.0.4" || len(fw.Refs) != 1 {
		t.Errorf("firewall = %+v", fw)
	}
	if ss := byName["Pool"]; ss.Attrs[azure.AttrCapacity] != "2" || len(ss.Refs) != 1 {
		t.Errorf("scale set = %+v", ss)
	}
	if d := byName["VM-1-os"]; d.Attrs[azure.AttrDiskSizeGB] != "128" || d.Refs[0].ID != strings.ToLower(vmID) {
		t.Errorf("disk = %+v", d)
	}
	if pe := byName["PE-KV"]; !strings.HasSuffix(pe.Attrs[azure.AttrLinkTargets], "/vaults/kv-1") {
		t.Errorf("private endpoint = %+v", pe)
	}

	// Secret-bearing columns are never selected.
	for _, c := range src.calls {
		for _, col := range []string{"secrets", "linux_configuration_ssh_public_keys", "access_policies", "tags"} {
			if strings.Contains(c.sql, " "+col+"::") {
				t.Errorf("query reads %s: %s", col, c.sql)
			}
		}
	}
}

func TestCollectSelection(t *testing.T) {
	sub := everything()
	sub.Resources.Compute = &config.AzureComputeResources{
		VirtualMachines: &config.AzureResourceQuery{IDs: []string{vmID}, Tags: map[string]string{"env": "prod"}},
	}
	sub.Resources.Network.Subnets = &config.AzureResourceQuery{IDs: []string{strings.ToUpper(subnetA)}}
	sub.Resources.Network.Firewalls = &config.AzureResourceQuery{ResourceGroups: []string{"rg-other"}}
	rows := testRows()
	vm := rows["azure_compute_virtual_machine"][0]
	vm["tags"] = row("tags", `{"env": "prod"}`)["tags"]
	untagged := base(vmID+"-2", "VM-2")
	untagged["tags"] = row("tags", `{"env": "dev"}`)["tags"]
	rows["azure_compute_virtual_machine"] = append(rows["azure_compute_virtual_machine"], untagged)
	src := &fakeSource{rows: rows}

	regions, err := Collect(context.Background(), src, sub)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range regions[0].Records {
		if r.Type == azure.TypeVirtualMachine || r.Type == azure.TypeSubnet || r.Type == azure.TypeFirewall {
			names = append(names, r.Name)
		}
	}
	if !reflect.DeepEqual(names, []string{"App", "VM-1"}) {
		t.Errorf("selected = %v, want the listed subnet and the tagged VM, and no firewall", names)
	}
	c := src.table("azure_compute_virtual_machine")
	if !strings.Contains(c.sql, "tags::text") || !strings.Contains(c.sql, "lower(id) = ANY($3)") ||
		!reflect.DeepEqual(c.args[2], []string{strings.ToLower(vmID)}) {
		t.Errorf("VM query = %s %v, want tags read and IDs matched lowercased", c.sql, c.args)
	}
	if src.table("azure_compute_disk") != nil {
		t.Error("an unnamed primary type was read")
	}
}

func TestCollectErrors(t *testing.T) {
	if _, err := Collect(context.Background(), &fakeSource{}, &config.AzureSubscription{}); err == nil {
		t.Error("no subscription ID did not fail")
	}
	boom := errors.New("boom")
	if _, err := Collect(context.Background(), &fakeSource{err: boom}, everything()); !errors.Is(err, boom) {
		t.Errorf("err = %v, want the query error", err)
	}
	for table, column := range map[string]string{
		"azure_virtual_network":                   "subnets",
		"azure_network_security_group":            "security_rules",
		"azure_network_interface":                 "ip_configurations",
		"azure_compute_virtual_machine":           "network_interfaces",
		"azure_compute_virtual_machine_scale_set": "virtual_machine_network_profile",
		"azure_firewall":                          "ip_configurations",
		"azure_private_endpoint":                  "subnet",
	} {
		rows := testRows()
		rows[table][0][column] = row(column, "{bad")[column]
		_, err := Collect(context.Background(), &fakeSource{rows: rows}, everything())
		if err == nil || !strings.Contains(err.Error(), "decode "+column) {
			t.Errorf("%s.%s: err = %v, want a decode error", table, column, err)
		}
	}
	rows := testRows()
	rows["azure_virtual_network"][0]["address_prefixes"] = row("a", "{bad")["a"]
	if _, err := Collect(context.Background(), &fakeSource{rows: rows}, everything()); err == nil {
		t.Error("bad address prefixes did not fail")
	}
	sub := everything()
	sub.Resources.Compute.VirtualMachines = &config.AzureResourceQuery{Tags: map[string]string{"a": "b"}}
	rows = testRows()
	rows["azure_compute_virtual_machine"][0]["tags"] = row("t", "{bad")["t"]
	if _, err := Collect(context.Background(), &fakeSource{rows: rows}, sub); err == nil {
		t.Error("bad tags did not fail")
	}
}

func TestMissingAndNarrow(t *testing.T) {
	r := &config.AzureResources{
		Network:          &config.AzureNetworkResources{NATGateways: &config.AzureResourceQuery{}},
		AppConfiguration: &config.AzureAppConfigurationResources{Stores: &config.AzureResourceQuery{}},
	}
	if got := missing(r); !reflect.DeepEqual(got, []string{"network.nat_gateways", "appconfiguration.stores"}) {
		t.Errorf("missing = %v", got)
	}
	if got := missing(&config.AzureResources{}); got != nil {
		t.Errorf("missing of nothing = %v", got)
	}

	region := &inventory.Region{Records: []inventory.Record{
		azure.Generic(azure.TypeSubnet, rg+"x/subnets/a", "a", nil),
		azure.Generic(azure.TypeSubnet, "/subscriptions/s/resourceGroups/other/x/subnets/b", "b", nil),
		azure.Generic(azure.TypeVirtualNetwork, rg+"x", "x", nil),
	}}
	narrowSubnets([]*inventory.Region{region}, &config.AzureResourceQuery{ResourceGroups: []string{"RG-APP"}})
	if len(region.Records) != 2 || region.Records[0].Name != "a" {
		t.Errorf("by resource group = %+v", region.Records)
	}
	narrowSubnets([]*inventory.Region{region}, &config.AzureResourceQuery{Tags: map[string]string{"a": "b"}})
	if len(region.Records) != 1 || region.Records[0].Type != azure.TypeVirtualNetwork {
		t.Errorf("by tag = %+v, want no subnets", region.Records)
	}
}
