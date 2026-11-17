package drawio

import (
	"strings"

	"diagram/internal/model"
	"diagram/internal/platform/azure"
)

// azureImages are the image paths of draw.io's Azure palette
// (img/lib/azure2 in its source). draw.io draws Azure icons as images, not
// stencils, and a path that does not exist draws a broken image, so each is
// checked against the source.
const azureImages = "img/lib/azure2/"

// azureIconStyle is draw.io's style for an Azure palette icon, with the label
// below.
const azureIconStyle = "image;aspect=fixed;html=1;points=[];align=center;fontSize=12;fontColor=" + textColor + ";" +
	"verticalLabelPosition=bottom;verticalAlign=top;image=" + azureImages

// azureIcons maps each Azure leaf resource type to its icon.
var azureIcons = map[model.ResourceType]string{
	azure.TypeVirtualMachine:  "compute/Virtual_Machine.svg",
	azure.TypeScaleSet:        "compute/VM_Scale_Sets.svg",
	azure.TypeDisk:            "compute/Disks.svg",
	azure.TypeFirewall:        "networking/Firewalls.svg",
	azure.TypePrivateEndpoint: "networking/Private_Endpoint.svg",
	azure.TypeKeyVault:        "security/Key_Vaults.svg",
	// Resource types read through include_all.
	azure.TypeManagedCluster:      "containers/Kubernetes_Services.svg",
	azure.TypeSQLServer:           "databases/SQL_Server.svg",
	azure.TypeLogAnalytics:        "analytics/Log_Analytics_Workspaces.svg",
	azure.TypeDiskEncryptionSet:   "compute/Disk_Encryption_Sets.svg",
	azure.TypeFirewallPolicy:      "networking/Azure_Firewall_Policy.svg",
	azure.TypeWebAppFirewallRules: "networking/Web_Application_Firewall_Policies_WAF.svg",
}

// azureTypes are the Azure resource types drawn by their own rules. Any
// other Azure type is generic: drawn with its type in its label.
var azureTypes = map[model.ResourceType]bool{
	azure.TypeVirtualNetwork: true, azure.TypeSubnet: true, azure.TypeNetworkSecurityGroup: true,
	azure.TypeVirtualMachine: true, azure.TypeScaleSet: true, azure.TypeDisk: true, azure.TypeFirewall: true,
	azure.TypePrivateEndpoint: true, azure.TypeKeyVault: true,
}

// azureBlue is the Azure brand blue, for virtual network outlines.
const azureBlue = "#0078D4"

// azureGroupStyle returns an Azure container style: a box with the type's
// icon at its top left and the label beside it. outline draws the border in
// stroke.
// draw.io's Azure palette has no group shapes, so its label shape carries
// the icon.
func azureGroupStyle(icon, stroke, fill, font string, outline bool) string {
	if !outline {
		stroke = "none"
	}
	style := "shape=label;html=1;whiteSpace=wrap;fontSize=12;fontStyle=0;container=1;pointerEvents=0;" +
		"collapsible=0;recursiveResize=0;verticalAlign=top;align=left;spacingLeft=30;spacingTop=2;" +
		"imageWidth=20;imageHeight=20;imageAlign=left;imageVerticalAlign=top;image=" + azureImages + icon +
		";strokeColor=" + stroke + ";fillColor=" + fill + ";fontColor=" + font + ";"
	return style
}

var (
	// azureRegionStyle is a plain dashed box: Azure has no region icon.
	azureRegionStyle = "rounded=0;html=1;whiteSpace=wrap;fontSize=12;container=1;pointerEvents=0;collapsible=0;" +
		"recursiveResize=0;verticalAlign=top;align=left;spacingLeft=10;fillColor=none;dashed=1;" +
		"strokeColor=#5E5E5E;fontColor=" + textColor + ";fontFamily=" + monoFont + ";"
	azureVNetStyle = azureVNetStyleFor(azureBlue)
)

// azureVNetStyleFor is a virtual network box outlined in color.
func azureVNetStyleFor(color string) string {
	return azureGroupStyle("networking/Virtual_Networks.svg", color, "none", color, true)
}

// azureSubnetStyle draws an Azure subnet in its color, as subnetStyle draws
// an AWS one: a tint of it as fill and a readable shade as label, no outline.
func azureSubnetStyle(color string, alt bool) string {
	fill := subnetFill(color, alt)
	return azureGroupStyle("networking/Subnet.svg", color, fill, textOn(color, fill), false)
}

// azureLeafLabel adds the facts an Azure resource's label shows below its
// name.
func azureLeafLabel(n *model.Node) []string {
	switch n.Type {
	case azure.TypeVirtualMachine, azure.TypeFirewall:
		return []string{n.Attrs[azure.AttrPrivateIP]}
	case azure.TypeDisk:
		if gb := n.Attrs[azure.AttrDiskSizeGB]; gb != "" {
			return []string{gb + " GB"}
		}
	case azure.TypeScaleSet:
		return []string{n.Attrs[azure.AttrSKU]}
	}
	return nil
}

// azureNodeData is the cell data for an Azure node: its ID, type, resource
// group, and attributes.
func azureNodeData(n *model.Node) [][2]string {
	a := n.Attrs
	data := [][2]string{
		{"azureId", n.ID}, {"type", string(n.Type)}, {"resourceGroup", a[azure.AttrResourceGroup]},
	}
	add := func(key, attr string) {
		if v := a[attr]; v != "" {
			data = append(data, [2]string{key, strings.ReplaceAll(v, ",", ", ")})
		}
	}
	add("cidr", azure.AttrCIDR)
	add("size", azure.AttrSize)
	add("osType", azure.AttrOSType)
	add("privateIp", azure.AttrPrivateIP)
	add("publicIps", azure.AttrPublicIPs)
	add("sku", azure.AttrSKU)
	add("capacity", azure.AttrCapacity)
	add("diskSizeGb", azure.AttrDiskSizeGB)
	add("vaultUri", azure.AttrVaultURI)
	add("linkTargets", azure.AttrLinkTargets)
	add("natGateway", azure.AttrNATGateway)
	add("networkSecurityGroup", azure.AttrNSG)
	return data
}
