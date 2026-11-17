package drawio

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"

	"diagram/internal/config"
	"diagram/internal/layout"
	"diagram/internal/model"
	"diagram/internal/platform/azure"
)

// azureModel is a virtual network with one subnet holding a VM in a network
// security group, a scale set, a disk, a firewall, and a private endpoint,
// plus a key vault and an AKS cluster in the region.
func azureModel() *model.Diagram {
	node := func(id, rt, parent string, attrs ...string) *model.Node {
		n := &model.Node{ID: id, Type: model.ResourceType(rt), Platform: azure.Platform, Name: id, Parent: parent,
			Attrs: map[string]string{}}
		for i := 0; i < len(attrs); i += 2 {
			n.Attrs[attrs[i]] = attrs[i+1]
		}
		return n
	}
	return &model.Diagram{Regions: []*model.Region{{
		Name: "eastus",
		Nodes: []*model.Node{
			node("vnet", azure.TypeVirtualNetwork, "", azure.AttrCIDR, "10.0.0.0/16,10.1.0.0/16"),
			node("snet", azure.TypeSubnet, "vnet", azure.AttrCIDR, "10.0.1.0/24", azure.AttrNSG, "nsg"),
			node("nsg", azure.TypeNetworkSecurityGroup, ""),
			node("vm", azure.TypeVirtualMachine, "snet", azure.AttrPrivateIP, "10.0.1.4",
				azure.AttrPublicIPs, "203.0.113.1,203.0.113.2", azure.AttrResourceGroup, "rg"),
			node("vmss", azure.TypeScaleSet, "snet", azure.AttrSKU, "Standard_D2s_v5"),
			node("disk", azure.TypeDisk, "snet", azure.AttrDiskSizeGB, "128"),
			node("disk-nosize", azure.TypeDisk, "snet"),
			node("fw", azure.TypeFirewall, "vnet", azure.AttrPrivateIP, "10.0.0.4"),
			node("pe", azure.TypePrivateEndpoint, "snet"),
			node("kv", azure.TypeKeyVault, ""),
			node("aks", azure.TypeManagedCluster, ""),
		},
		Edges: []*model.Edge{{Relation: model.RelMemberOf, From: "vm", To: "nsg"}},
	}}}
}

func TestRenderAzure(t *testing.T) {
	colors := config.Colors{Resources: map[string]string{"vnet": "#E7157B"}}
	out, err := Render(context.Background(), "az", azureModel(), layout.ELK{}, colors)
	if err != nil {
		t.Fatal(err)
	}
	var p parsed
	if err := xml.Unmarshal(out, &p); err != nil {
		t.Fatalf("output is not valid XML: %v", err)
	}
	styles, labels, parents := map[string]string{}, map[string]string{}, map[string]string{}
	for _, o := range p.Objects {
		styles[o.ID], labels[o.ID], parents[o.ID] = o.Cell.Style, o.Label, o.Cell.Parent
	}

	for id, want := range map[string]string{
		"eastus": "strokeColor=#5E5E5E",
		"vnet":   "Virtual_Networks.svg;strokeColor=#E7157B",
		"snet":   "Subnet.svg;strokeColor=none",
		"vm":     "image=img/lib/azure2/compute/Virtual_Machine.svg;",
		"vmss":   "VM_Scale_Sets.svg",
		"kv":     "security/Key_Vaults.svg",
		"aks":    "containers/Kubernetes_Services.svg",
	} {
		if !strings.Contains(styles[id], want) {
			t.Errorf("%s style = %q, want it to hold %q", id, styles[id], want)
		}
	}
	for id, want := range map[string]string{
		"vnet":        "vnet (" + mono("10.0.0.0/16, 10.1.0.0/16") + ")",
		"vm":          "vm<br>" + mono("10.0.1.4"),
		"vmss":        "vmss<br>" + mono("Standard_D2s_v5"),
		"disk":        "disk<br>" + mono("128 GB"),
		"disk-nosize": "disk-nosize",
		"fw":          "fw<br>" + mono("10.0.0.4"),
		"pe":          "pe",
		"aks":         "aks<br>" + mono(string(azure.TypeManagedCluster)),
	} {
		if labels[id] != want {
			t.Errorf("%s label = %q, want %q", id, labels[id], want)
		}
	}
	if parents["vm"] != "nsg-in-snet" || parents["nsg-in-snet"] != "snet" {
		t.Errorf("vm in %q, its box in %q; want the NSG box inside the subnet", parents["vm"], parents["nsg-in-snet"])
	}
	for _, want := range []string{
		`azureId="vm"`, `resourceGroup="rg"`, `publicIps="203.0.113.1, 203.0.113.2"`,
		`type="Network/networkSecurityGroups" azureId="nsg"`, `networkSecurityGroup="nsg"`,
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output does not hold %s", want)
		}
	}
	if strings.Contains(string(out), "awsId") {
		t.Error("Azure cells carry awsId")
	}
}

func TestPlatformOfEmptyRegion(t *testing.T) {
	if got := platformOf(&model.Region{}); got != "" {
		t.Errorf("platformOf(empty) = %q", got)
	}
}
