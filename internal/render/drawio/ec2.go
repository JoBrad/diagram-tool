package drawio

import (
	"fmt"
	"slices"
	"strings"

	"diagram/internal/inventory"
	"diagram/internal/layout"
	"diagram/internal/model"
	"diagram/internal/platform/aws"
	"diagram/internal/platform/azure"
)

// Resource sizes, in points. The layout reserves a slot wider and taller
// than the icon, for its label and so links leaving it spread out; the icon
// is drawn at the top center of its slot.
const (
	iconSize   = 60
	slotWidth  = 160
	slotHeight = 100
)

// iconStyle is the shared style for AWS resource icons, with the label below.
const iconStyle = "sketch=0;outlineConnect=0;fontColor=" + textColor +
	";gradientColor=none;strokeColor=none;dashed=0;" +
	"verticalLabelPosition=bottom;verticalAlign=top;align=center;html=1;fontSize=12;fontStyle=0;" +
	"aspect=fixed;pointerEvents=1;"

// icons maps each leaf resource type to its draw.io AWS shape and fill color.
var icons = map[model.ResourceType][2]string{
	aws.TypeInstance:        {"mxgraph.aws4.instance2", "#ED7100"},
	aws.TypeNATGateway:      {"mxgraph.aws4.nat_gateway", "#8C4FFF"},
	aws.TypeVPNGateway:      {"mxgraph.aws4.vpn_gateway", "#8C4FFF"},
	aws.TypeInternetGateway: {"mxgraph.aws4.internet_gateway", "#8C4FFF"},
	aws.TypeTransitGateway:  {"mxgraph.aws4.transit_gateway", "#8C4FFF"},
	aws.TypeCustomerGateway: {"mxgraph.aws4.customer_gateway", "#8C4FFF"},
	// Resource types read through include_all.
	"AWS::RDS::DBInstance": {"mxgraph.aws4.rds_instance", "#C925D1"},
}

// resourceIconStyle is draw.io's style for resource icons: a glyph drawn in
// the stroke color on a square tile of the fill color. Both colors and the
// fixed aspect come from the palette; without them the tile disappears or
// stretches.
const resourceIconStyle = "sketch=0;outlineConnect=0;fontColor=" + textColor + ";strokeColor=#ffffff;dashed=0;" +
	"verticalLabelPosition=bottom;verticalAlign=top;align=center;html=1;fontSize=12;fontStyle=0;aspect=fixed;" +
	"shape=mxgraph.aws4.resourceIcon;"

// resourceIcons maps resource types drawn as resource icons to their resIcon and tile
// color, as draw.io's AWS palette (Sidebar-AWS4.js) defines them.
var resourceIcons = map[model.ResourceType][2]string{
	"AWS::ElasticLoadBalancingV2::LoadBalancer": {"mxgraph.aws4.elastic_load_balancing", "#8C4FFF"},
	"AWS::Lambda::Function":                     {"mxgraph.aws4.lambda", "#ED7100"},
	"AWS::RDS::DBCluster":                       {"mxgraph.aws4.rds", "#C925D1"},
}

// iconShape returns the draw.io shape for a leaf: the instance family's icon
// when draw.io has one, else the resource type's generic icon.
func iconShape(n *model.Node) string {
	if n.Type == aws.TypeInstance {
		family, _, _ := strings.Cut(n.Attrs[aws.AttrInstanceType], ".")
		if shape, ok := instanceShapes[family]; ok {
			return "mxgraph.aws4." + shape
		}
		if shape := sharedInstanceShape(family); shape != "" {
			return "mxgraph.aws4." + shape
		}
	}
	return icons[n.Type][0]
}

// sharedInstanceShape returns an icon that covers a group of families, for
// families without their own: Mac instances by chip, high-memory
// instances, Trainium, and compute-optimized (C) families.
func sharedInstanceShape(family string) string {
	switch {
	case family == "mac2":
		return "ec2_m1_mac_instance"
	case strings.HasPrefix(family, "mac"):
		return "mac_instance"
	case strings.HasPrefix(family, "u-"), strings.HasPrefix(family, "u7"):
		return "high_memory_instance"
	case strings.HasPrefix(family, "trn"):
		return "trainium_instance"
	case len(family) > 1 && family[0] == 'c' && family[1] >= '0' && family[1] <= '9':
		return "optimized_instance"
	default:
		return ""
	}
}

// groupStyle returns an AWS group container style. outline draws the
// border; AWS's own subnet style is a filled box without one.
func groupStyle(icon, stroke, fill, font string, dashed, outline bool) string {
	style := "outlineConnect=0;gradientColor=none;html=1;whiteSpace=wrap;fontSize=12;fontStyle=0;" +
		"container=1;pointerEvents=0;collapsible=0;recursiveResize=0;shape=mxgraph.aws4.group;" +
		"grIcon=" + icon + ";strokeColor=" + stroke +
		";fillColor=" + fill + ";verticalAlign=top;align=left;spacingLeft=30;fontColor=" + font + ";"
	if dashed {
		style += "dashed=1;"
	}
	if !outline {
		style += "grStroke=0;"
	}
	return style
}

var (
	regionStyle = groupStyle("mxgraph.aws4.group_region", "#00A4A6", "none", "#147EBA", true, true) +
		"fontFamily=" + monoFont + ";"
	vpcStyle        = groupStyle("mxgraph.aws4.group_vpc2", "#8C4FFF", "none", textColor, false, true)
	prefixListStyle = "rounded=1;whiteSpace=wrap;html=1;fillColor=#F5F5F5;strokeColor=#666666;fontColor=" + textColor + ";"
	vpnStyle        = edgeRouting + "html=1;rounded=0;strokeColor=#8C4FFF;strokeWidth=2;" +
		"startArrow=none;endArrow=classic;"
)

// vpcStyleFor returns the box style of a VPC or Azure virtual network, in
// the config's color for it when there is one.
func vpcStyleFor(n *model.Node, colors map[string]string) string {
	c, colored := colors[n.ID]
	switch {
	case n.Platform == azure.Platform && colored:
		return azureVNetStyleFor(c)
	case n.Platform == azure.Platform:
		return azureVNetStyle
	case colored:
		return groupStyle("mxgraph.aws4.group_vpc2", c, "none", c, false, true)
	}
	return vpcStyle
}

// edgeCells returns the region's VPN connections and security group rule
// links, as layout links and their cells. present holds the drawn IDs.
func edgeCells(
	r *model.Region, present map[string]bool, boxes []*sgBox, origin func(string) string,
) ([]layout.Link, []cell) {
	var links []layout.Link
	var cells []cell
	for _, e := range r.EdgesOf(aws.RelVPNConnection) {
		if !present[e.From] || !present[e.To] {
			warnf(r.Name, "skip %s: gateway %q or customer gateway %q is not in the diagram", e.ID, e.From, e.To)
			continue
		}
		links = append(links, layout.Link{ID: e.ID, From: e.From, To: e.To})
		cells = append(cells, cell{id: e.ID, edge: true, source: e.From, target: e.To, style: vpnStyle,
			data: [][2]string{{"type", string(e.Relation)}, {"awsId", e.ID}}})
	}
	for _, l := range ruleLinks(r, present, boxes) {
		links = append(links, l)
		cells = append(cells, cell{id: l.ID, edge: true, source: l.From, target: l.To, label: l.Label,
			style: ruleStyleFor(origin(l.From)), data: [][2]string{{"type", "security_group_rule"}}})
	}
	return links, cells
}

// edgeRouting is the routing part of every edge style. libavoidRouting marks
// the edge for draw.io's obstacle-avoiding router: draw.io reroutes it around
// shapes when a connected shape moves, and owns the waypoints it writes. A
// file opens with plain orthogonal edges until then; Arrange > Layout >
// Orthogonal Routing routes them all at once.
const edgeRouting = "edgeStyle=orthogonalEdgeStyle;libavoidRouting=1;"

// genericStyle draws a resource of a resource type with no verified draw.io shape as a
// plain labeled box. Guessing a shape name draws an empty box instead.
const genericStyle = "rounded=1;whiteSpace=wrap;html=1;fillColor=#F5F5F5;strokeColor=#666666;" +
	"fontColor=" + textColor + ";fontSize=11;"

// leafStyle returns the style for a leaf node and whether it is drawn as an
// icon, or false for a node that is not drawn as a leaf. A generic box fills
// its slot; an icon is drawn iconSize square with its label below.
func leafStyle(n *model.Node) (style string, icon, ok bool) {
	if shape, ok := icons[n.Type]; ok {
		return iconStyle + "shape=" + iconShape(n) + ";fillColor=" + shape[1] + ";", true, true
	}
	if res, ok := resourceIcons[n.Type]; ok {
		return resourceIconStyle + "resIcon=" + res[0] + ";fillColor=" + res[1] + ";", true, true
	}
	if img, ok := azureIcons[n.Type]; ok {
		return azureIconStyle + img + ";", true, true
	}
	if isGeneric(n) {
		return genericStyle, false, true
	}
	return "", false, false
}

// ec2Types are the resource types drawn by their own rules. Any other type,
// such as one read through include_all, is generic: drawn as a leaf with its
// type in its label.
var ec2Types = map[model.ResourceType]bool{
	aws.TypeInstance: true, aws.TypeNATGateway: true, aws.TypeVPNGateway: true, aws.TypeInternetGateway: true,
	aws.TypeTransitGateway: true, aws.TypeCustomerGateway: true, aws.TypePrefixList: true, aws.TypeVPC: true,
	aws.TypeSubnet: true, aws.TypeSecurityGroup: true,
}

// isGeneric reports whether n is of a resource type with no drawing rules
// of its own.
func isGeneric(n *model.Node) bool {
	return !ec2Types[n.Type] && !azureTypes[n.Type]
}

// leafLabel is a resource's display text, one fact per line.
func leafLabel(n *model.Node) string {
	lines := []string{n.Name}
	switch {
	case n.Type == aws.TypeInstance:
		lines = append(lines, n.Attrs[aws.AttrPrivateIP])
	case n.Type == aws.TypeNATGateway:
		lines = append(lines, strings.Split(n.Attrs[aws.AttrPublicIPs], ",")...)
	case n.Type == aws.TypeCustomerGateway:
		lines = append(lines, n.Attrs[aws.AttrIP])
	case n.Platform == azure.Platform && !isGeneric(n):
		lines = append(lines, azureLeafLabel(n)...)
	case isGeneric(n):
		lines = append(lines, string(n.Type))
	}
	return strings.Join(slices.DeleteFunc(lines, func(s string) bool { return s == "" }), "\n")
}

// leafCellLabel turns a leafLabel into an html=1 label: line breaks, and the
// literal values under the name in the mono font.
func leafCellLabel(label string) string {
	lines := strings.Split(label, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = mono(lines[i])
	}
	return strings.Join(lines, "<br>")
}

// nodeData is the cell data for a node: its cloud ID, type, and attributes.
func nodeData(n *model.Node) [][2]string {
	if n.Platform == azure.Platform {
		return azureNodeData(n)
	}
	data := [][2]string{{"awsId", n.ID}, {"type", string(n.Type)}}
	a := n.Attrs
	switch n.Type {
	case aws.TypeInstance:
		data = append(
			data,
			[2]string{"instanceType", a[aws.AttrInstanceType]},
			[2]string{"platform", a[aws.AttrPlatformDetails]},
			[2]string{"privateIp", a[aws.AttrPrivateIP]},
			[2]string{"publicIp", a[aws.AttrPublicIP]},
		)
	case aws.TypeNATGateway:
		data = append(data, [2]string{"publicIps", strings.ReplaceAll(a[aws.AttrPublicIPs], ",", ", ")})
	case aws.TypeCustomerGateway:
		data = append(data, [2]string{"ip", a[aws.AttrIP]})
	case aws.TypeVPC, aws.TypeSubnet:
		data = append(data, [2]string{"cidr", a[aws.AttrCIDR]})
		if n.Zone != "" {
			data = append(
				data,
				[2]string{"availabilityZone", n.Zone},
				[2]string{"public", fmt.Sprint(n.Tier == inventory.TierPublic)},
			)
		}
	}
	return data
}

// securityGroups returns each instance's security groups as "name (id)".
func securityGroups(r *model.Region) map[string][]string {
	names := map[string]string{}
	for _, n := range ofKind(r, isSecurityGroup) {
		names[n.ID] = fmt.Sprintf("%s (%s)", n.Name, n.ID)
	}
	out := map[string][]string{}
	for _, e := range r.EdgesOf(model.RelMemberOf) {
		if name, ok := names[e.To]; ok {
			out[e.From] = append(out[e.From], name)
		}
	}
	return out
}

// portLabel formats a rule as "tcp/443", "tcp/1024-65535", "icmp", or "all".
func portLabel(rule model.Rule) string {
	if rule.Protocol == "-1" {
		return "all"
	}
	if rule.FromPort == nil || *rule.FromPort == -1 {
		return rule.Protocol
	}
	if rule.ToPort == nil || *rule.FromPort == *rule.ToPort {
		return fmt.Sprintf("%s/%d", rule.Protocol, *rule.FromPort)
	}
	return fmt.Sprintf("%s/%d-%d", rule.Protocol, *rule.FromPort, *rule.ToPort)
}
