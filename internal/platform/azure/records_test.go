package azure

import (
	"reflect"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v8"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v9"

	"diagram/internal/inventory"
)

// Hand-built IDs, in the mixed case Azure returns them in.
const (
	rgPath  = "/subscriptions/00000000-0000-0000-0000-000000000001/resourceGroups/RG-App/providers/"
	vnetID  = rgPath + "Microsoft.Network/virtualNetworks/VNet-1"
	subnetA = vnetID + "/subnets/App"
	nsgID   = rgPath + "Microsoft.Network/networkSecurityGroups/NSG-App"
	nicID   = rgPath + "Microsoft.Network/networkInterfaces/vm-1-nic"
	pipID   = rgPath + "Microsoft.Network/publicIPAddresses/vm-1-ip"
	vmID    = rgPath + "Microsoft.Compute/virtualMachines/VM-1"
)

// low lowercases an ID as the builders do.
func low(s string) string {
	return id(&s)
}

func TestVirtualNetworkAndSubnets(t *testing.T) {
	v := armnetwork.VirtualNetwork{
		ID: ptr(vnetID), Name: ptr("VNet-1"),
		Properties: &armnetwork.VirtualNetworkPropertiesFormat{
			AddressSpace: &armnetwork.AddressSpace{AddressPrefixes: []*string{ptr("10.0.0.0/16"), ptr("10.1.0.0/16")}},
			Subnets: []*armnetwork.Subnet{
				{
					ID: ptr(subnetA), Name: ptr("App"),
					Properties: &armnetwork.SubnetPropertiesFormat{
						AddressPrefix:        ptr("10.0.1.0/24"),
						NetworkSecurityGroup: &armnetwork.SecurityGroup{ID: ptr(nsgID)},
						NatGateway: &armnetwork.SubResource{
							ID: ptr(rgPath + "Microsoft.Network/natGateways/NAT"),
						},
					},
				},
				{ID: ptr(vnetID + "/subnets/Data")},
				nil,
			},
		},
	}
	net := VirtualNetwork(v)
	if net.ID != low(vnetID) || net.Type != TypeVirtualNetwork || net.Platform != Platform ||
		net.Attrs[AttrCIDR] != "10.0.0.0/16,10.1.0.0/16" || net.Attrs[AttrResourceGroup] != "rg-app" {
		t.Errorf("VirtualNetwork = %+v", net)
	}
	subnets := Subnets(v)
	if len(subnets) != 2 {
		t.Fatalf("Subnets = %+v, want 2", subnets)
	}
	app := subnets[0]
	wantRefs := []inventory.Ref{
		{Relation: inventory.RelContainedIn, Type: TypeVirtualNetwork, ID: low(vnetID)},
		{Relation: RelSubnetNSG, Type: TypeNetworkSecurityGroup, ID: low(nsgID)},
	}
	if app.Name != "App" || app.Attrs[AttrCIDR] != "10.0.1.0/24" || !reflect.DeepEqual(app.Refs, wantRefs) ||
		app.Attrs[AttrNSG] != low(nsgID) || app.Attrs[AttrNATGateway] == "" || app.Zone != "" || app.Tier != "" {
		t.Errorf("subnet = %+v", app)
	}
	if subnets[1].Name != low(vnetID+"/subnets/Data") {
		t.Errorf("unnamed subnet name = %q, want its ID", subnets[1].Name)
	}
	if Subnets(armnetwork.VirtualNetwork{}) != nil {
		t.Error("a network without properties has subnets")
	}
}

func TestNetworkInterface(t *testing.T) {
	cfg := func(primary bool, ip, subnet, pip string) *armnetwork.InterfaceIPConfiguration {
		p := &armnetwork.InterfaceIPConfigurationPropertiesFormat{
			Primary: &primary, PrivateIPAddress: ptr(ip), Subnet: &armnetwork.Subnet{ID: ptr(subnet)},
		}
		if pip != "" {
			p.PublicIPAddress = &armnetwork.PublicIPAddress{ID: ptr(pip)}
		}
		return &armnetwork.InterfaceIPConfiguration{Properties: p}
	}
	n := NetworkInterface(armnetwork.Interface{
		ID: ptr(nicID), Name: ptr("vm-1-nic"),
		Properties: &armnetwork.InterfacePropertiesFormat{
			IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
				cfg(false, "10.0.2.4", vnetID+"/subnets/Second", ""),
				cfg(true, "10.0.1.4", subnetA, pipID),
				{},
			},
			NetworkSecurityGroup: &armnetwork.SecurityGroup{ID: ptr(nsgID)},
		},
	})
	want := []inventory.Ref{
		{Relation: inventory.RelContainedIn, Type: TypeSubnet, ID: low(subnetA)},
		{Relation: RelPublicIP, Type: TypePublicIP, ID: low(pipID)},
		{Relation: inventory.RelContainedIn, Type: TypeSubnet, ID: low(vnetID + "/subnets/Second")},
		{Relation: inventory.RelMemberOf, Type: TypeNetworkSecurityGroup, ID: low(nsgID)},
	}
	if n.Attrs[AttrPrivateIP] != "10.0.1.4" || !reflect.DeepEqual(n.Refs, want) {
		t.Errorf("NetworkInterface = %+v", n)
	}
	if bare := NetworkInterface(armnetwork.Interface{ID: ptr(nicID)}); len(bare.Refs) != 0 {
		t.Errorf("bare interface = %+v", bare)
	}
}

func TestVirtualMachine(t *testing.T) {
	size, os := armcompute.VirtualMachineSizeTypesStandardB2S, armcompute.OperatingSystemTypesLinux
	vm := VirtualMachine(armcompute.VirtualMachine{
		ID: ptr(vmID), Name: ptr("VM-1"),
		Properties: &armcompute.VirtualMachineProperties{
			HardwareProfile: &armcompute.HardwareProfile{VMSize: &size},
			StorageProfile:  &armcompute.StorageProfile{OSDisk: &armcompute.OSDisk{OSType: &os}},
			NetworkProfile: &armcompute.NetworkProfile{NetworkInterfaces: []*armcompute.NetworkInterfaceReference{
				{ID: ptr(nicID + "-2")},
				{ID: ptr(nicID), Properties: &armcompute.NetworkInterfaceReferenceProperties{Primary: ptr(true)}},
			}},
		},
	})
	if vm.Attrs[AttrSize] != "Standard_B2s" || vm.Attrs[AttrOSType] != "Linux" ||
		len(vm.Refs) != 2 || vm.Refs[0].ID != low(nicID) || vm.Refs[0].Relation != RelNetworkInterface {
		t.Errorf("VirtualMachine = %+v", vm)
	}
	if bare := VirtualMachine(armcompute.VirtualMachine{ID: ptr(vmID)}); bare.Name != low(vmID) {
		t.Errorf("bare VM = %+v", bare)
	}
}

func TestScaleSet(t *testing.T) {
	ipConfig := func(subnet string) *armcompute.VirtualMachineScaleSetIPConfiguration {
		return &armcompute.VirtualMachineScaleSetIPConfiguration{
			Properties: &armcompute.VirtualMachineScaleSetIPConfigurationProperties{
				Subnet: &armcompute.APIEntityReference{ID: ptr(subnet)},
			},
		}
	}
	s := ScaleSet(armcompute.VirtualMachineScaleSet{
		ID: ptr(rgPath + "Microsoft.Compute/virtualMachineScaleSets/Pool"), Name: ptr("Pool"),
		SKU: &armcompute.SKU{Name: ptr("Standard_D2s_v5"), Capacity: ptr(int64(3))},
		Properties: &armcompute.VirtualMachineScaleSetProperties{
			VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
				NetworkProfile: &armcompute.VirtualMachineScaleSetNetworkProfile{
					NetworkInterfaceConfigurations: []*armcompute.VirtualMachineScaleSetNetworkConfiguration{
						{Properties: &armcompute.VirtualMachineScaleSetNetworkConfigurationProperties{
							Primary: ptr(true),
							IPConfigurations: []*armcompute.VirtualMachineScaleSetIPConfiguration{
								ipConfig(subnetA),
								nil,
							},
							NetworkSecurityGroup: &armcompute.SubResource{ID: ptr(nsgID)},
						}},
						{},
					},
				},
			},
		},
	})
	want := []inventory.Ref{
		{Relation: inventory.RelContainedIn, Type: TypeSubnet, ID: low(subnetA)},
		{Relation: inventory.RelMemberOf, Type: TypeNetworkSecurityGroup, ID: low(nsgID)},
	}
	if s.Attrs[AttrSKU] != "Standard_D2s_v5" || s.Attrs[AttrCapacity] != "3" || !reflect.DeepEqual(s.Refs, want) {
		t.Errorf("ScaleSet = %+v", s)
	}
	if bare := ScaleSet(armcompute.VirtualMachineScaleSet{ID: ptr("x")}); len(bare.Refs) != 0 {
		t.Errorf("bare scale set = %+v", bare)
	}
}

func TestFirewallEndpointDiskVault(t *testing.T) {
	tier := armnetwork.AzureFirewallSKUTierStandard
	fw := Firewall(armnetwork.AzureFirewall{
		ID: ptr(rgPath + "Microsoft.Network/azureFirewalls/FW"), Name: ptr("FW"),
		Properties: &armnetwork.AzureFirewallPropertiesFormat{
			SKU: &armnetwork.AzureFirewallSKU{Tier: &tier},
			IPConfigurations: []*armnetwork.AzureFirewallIPConfiguration{{
				Properties: &armnetwork.AzureFirewallIPConfigurationPropertiesFormat{
					PrivateIPAddress: ptr("10.0.0.4"),
					Subnet:           &armnetwork.SubResource{ID: ptr(vnetID + "/subnets/AzureFirewallSubnet")},
					PublicIPAddress:  &armnetwork.SubResource{ID: ptr(pipID)},
				},
			}, nil},
		},
	})
	if fw.Attrs[AttrSKU] != "Standard" || fw.Attrs[AttrPrivateIP] != "10.0.0.4" || len(fw.Refs) != 2 ||
		fw.Refs[1].Relation != RelPublicIP {
		t.Errorf("Firewall = %+v", fw)
	}

	pe := PrivateEndpoint(armnetwork.PrivateEndpoint{
		ID: ptr(rgPath + "Microsoft.Network/privateEndpoints/PE"), Name: ptr("PE"),
		Properties: &armnetwork.PrivateEndpointProperties{
			Subnet: &armnetwork.Subnet{ID: ptr(subnetA)},
			PrivateLinkServiceConnections: []*armnetwork.PrivateLinkServiceConnection{{
				Properties: &armnetwork.PrivateLinkServiceConnectionProperties{
					PrivateLinkServiceID: ptr(rgPath + "Microsoft.KeyVault/vaults/KV"),
				},
			}},
			ManualPrivateLinkServiceConnections: []*armnetwork.PrivateLinkServiceConnection{nil},
		},
	})
	if pe.Attrs[AttrLinkTargets] != low(rgPath+"Microsoft.KeyVault/vaults/KV") || len(pe.Refs) != 1 {
		t.Errorf("PrivateEndpoint = %+v", pe)
	}

	sku := armcompute.DiskStorageAccountTypesPremiumLRS
	disk := Disk(armcompute.Disk{
		ID: ptr(rgPath + "Microsoft.Compute/disks/OS"), ManagedBy: ptr(vmID),
		SKU: &armcompute.DiskSKU{Name: &sku}, Properties: &armcompute.DiskProperties{DiskSizeGB: ptr(int32(128))},
	})
	if disk.Attrs[AttrSKU] != "Premium_LRS" || disk.Attrs[AttrDiskSizeGB] != "128" ||
		!reflect.DeepEqual(
			disk.Refs,
			[]inventory.Ref{{Relation: RelManagedBy, Type: TypeVirtualMachine, ID: low(vmID)}},
		) {
		t.Errorf("Disk = %+v", disk)
	}
	if loose := Disk(armcompute.Disk{ID: ptr("d"), ManagedBy: ptr("")}); len(loose.Refs) != 0 {
		t.Errorf("unattached disk = %+v", loose)
	}

	kv := KeyVault(rgPath+"Microsoft.KeyVault/vaults/KV", "KV", "https://kv.vault.azure.net/")
	if kv.ID != low(rgPath+"Microsoft.KeyVault/vaults/KV") || kv.Attrs[AttrVaultURI] == "" ||
		kv.Attrs[AttrResourceGroup] != "rg-app" {
		t.Errorf("KeyVault = %+v", kv)
	}

	for _, bare := range []inventory.Record{
		Firewall(armnetwork.AzureFirewall{ID: ptr("f")}),
		PrivateEndpoint(armnetwork.PrivateEndpoint{ID: ptr("p")}),
		NetworkSecurityGroup(armnetwork.SecurityGroup{ID: ptr("n")}),
		PublicIP(armnetwork.PublicIPAddress{ID: ptr("i")}),
	} {
		if len(bare.Refs) != 0 || len(bare.Rules) != 0 {
			t.Errorf("bare %s = %+v", bare.Type, bare)
		}
	}
}

func TestGeneric(t *testing.T) {
	g := Generic(TypeManagedCluster, rgPath+"Microsoft.ContainerService/managedClusters/AKS", "AKS", []string{subnetA})
	if g.ID != low(rgPath+"Microsoft.ContainerService/managedClusters/AKS") || g.Type != TypeManagedCluster ||
		len(g.Refs) != 1 || g.Refs[0].ID != low(subnetA) {
		t.Errorf("Generic = %+v", g)
	}
	if resourceGroup("no-group") != "" || vnetOf(low(vnetID)) != "" {
		t.Error("an ID with no resource group or subnet parsed as one")
	}
}
