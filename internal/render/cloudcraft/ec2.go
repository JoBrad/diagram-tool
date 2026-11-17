package cloudcraft

import (
	"fmt"
	"strings"

	"diagram/internal/inventory"
	"diagram/internal/model"
	"diagram/internal/platform/aws"
)

// vpcOf returns the VPC n sits in, at any depth, or "".
func vpcOf(r *model.Region, n *model.Node) string {
	if v := r.Ancestor(n, func(p *model.Node) bool { return p.Type == aws.TypeVPC }); v != nil {
		return v.ID
	}
	return ""
}

// subnetOf returns the subnet n sits in directly, or "".
func subnetOf(r *model.Region, n *model.Node) string {
	if p := r.Node(n.Parent); p != nil && p.Type == aws.TypeSubnet {
		return p.ID
	}
	return ""
}

// rules returns a security group's inbound or outbound rules.
func rules(n *model.Node, egress bool) []model.Rule {
	var out []model.Rule
	for _, rule := range n.Rules {
		if rule.Egress == egress {
			out = append(out, rule)
		}
	}
	return out
}

func drawInstances(r *model.Region, d *diagram) {
	for _, n := range r.Of(aws.TypeInstance) {
		family, size, _ := strings.Cut(n.Attrs[aws.AttrInstanceType], ".")
		d.addNode(component{
			"id":           n.ID,
			"type":         "ec2",
			"region":       d.region,
			"platform":     ec2Platform(n.Attrs[aws.AttrPlatformDetails]),
			"instanceType": family,
			"instanceSize": size,
			"transparent":  false,
		}, placement{vpc: vpcOf(r, n), subnet: subnetOf(r, n), tier: tierCompute})
		d.label(n.ID, n.Name+"\n"+n.Attrs[aws.AttrPrivateIP])
	}
}

func drawNATGateways(r *model.Region, d *diagram) {
	for _, n := range r.Of(aws.TypeNATGateway) {
		d.addNode(component{
			"id":        n.ID,
			"type":      "natgateway",
			"region":    d.region,
			"direction": "down",
		}, placement{vpc: vpcOf(r, n), subnet: subnetOf(r, n), tier: tierCompute})
		label := n.Name
		if ips := n.Attrs[aws.AttrPublicIPs]; ips != "" {
			label += "\n" + strings.ReplaceAll(ips, ",", "\n")
		}
		d.label(n.ID, label)
	}
}

// ec2Platform maps an instance's PlatformDetails to a Cloudcraft platform.
func ec2Platform(details string) string {
	details = strings.ToLower(details)
	switch {
	case strings.Contains(details, "windows"):
		return "mswin"
	case strings.Contains(details, "red hat"):
		return "rhel"
	case strings.Contains(details, "suse"):
		return "sles"
	default:
		return "linux"
	}
}

// drawGateways draws each node of resource type rt as a Cloudcraft node of typ in the
// gateway row, under its VPC when attached to one.
func drawGateways(r *model.Region, d *diagram, rt model.ResourceType, typ string) {
	for _, n := range r.Of(rt) {
		d.addNode(component{
			"id":        n.ID,
			"type":      typ,
			"region":    d.region,
			"direction": "down",
		}, placement{vpc: vpcOf(r, n), tier: tierGateway})
		d.label(n.ID, n.Name)
	}
}

func drawVPNGateways(r *model.Region, d *diagram) {
	drawGateways(r, d, aws.TypeVPNGateway, "vpngateway")
}

func drawInternetGateways(r *model.Region, d *diagram) {
	drawGateways(r, d, aws.TypeInternetGateway, "internetgateway")
}

func drawTransitGateways(r *model.Region, d *diagram) {
	for _, n := range r.Of(aws.TypeTransitGateway) {
		connections := 0
		for _, e := range r.EdgesOf(aws.RelVPNConnection) {
			if e.From == n.ID {
				connections++
			}
		}
		d.addNode(component{
			"id":          n.ID,
			"type":        "transitgateway",
			"region":      d.region,
			"direction":   "down",
			"connections": connections,
		}, placement{tier: tierGateway})
		d.label(n.ID, n.Name)
	}
}

func drawCustomerGateways(r *model.Region, d *diagram) {
	for _, n := range r.Of(aws.TypeCustomerGateway) {
		d.addNode(component{
			"id":        n.ID,
			"type":      "customergateway",
			"region":    d.region,
			"direction": "down",
		}, placement{tier: tierExternal})
		d.label(n.ID, n.Name+"\n"+n.Attrs[aws.AttrIP])
	}
}

// drawContainerPlaceholders adds a placeholder node to each subnet, security
// group, then VPC, that has no other node in it. Cloudcraft sizes a group
// around its members and does not render an empty one; Cloudcraft's own
// diagrams use placeholder nodes the same way.
func drawContainerPlaceholders(r *model.Region, d *diagram) {
	occupied := map[string]bool{}
	for _, p := range d.placements {
		occupied[p.subnet] = true
		occupied[p.vpc] = true
	}

	for _, n := range r.Of(aws.TypeSubnet) {
		if occupied[n.ID] {
			continue
		}
		vpc := vpcOf(r, n)
		d.addPlaceholder(n.ID, placement{vpc: vpc, subnet: n.ID, tier: tierCompute})
		occupied[vpc] = true
	}
	members := sgMembers(r, d)
	for _, n := range r.Of(aws.TypeSecurityGroup) {
		if len(members[n.ID]) > 0 {
			continue
		}
		vpc := vpcOf(r, n)
		d.addPlaceholder(n.ID, placement{vpc: vpc, tier: tierCompute})
		occupied[vpc] = true
	}
	for _, n := range r.Of(aws.TypeVPC) {
		if !occupied[n.ID] {
			d.addPlaceholder(n.ID, placement{vpc: n.ID, tier: tierCompute})
		}
	}
}

// addPlaceholder adds a placeholder node that holds open the container
// containerID.
func (d *diagram) addPlaceholder(containerID string, p placement) {
	d.addNode(component{
		"id":   containerID + "-placeholder",
		"type": "placeholder",
	}, p)
}

// sgMembers returns the drawn instances in each security group, keyed by
// group ID.
func sgMembers(r *model.Region, d *diagram) map[string][]string {
	members := map[string][]string{}
	for _, e := range r.EdgesOf(model.RelMemberOf) {
		if d.drawn[e.From] {
			members[e.To] = append(members[e.To], e.From)
		}
	}
	return members
}

// drawPrefixLists adds an unlabeled placeholder for each prefix list.
// linkSecurityGroups wraps it in a stand-in security group, because a
// Cloudcraft rule can target a group but not a prefix list. A text label
// anchored to the placeholder made Cloudcraft crash when it was clicked.
func drawPrefixLists(r *model.Region, d *diagram) {
	for _, n := range r.Of(aws.TypePrefixList) {
		d.addPlaceholder(n.ID, placement{tier: tierExternal})
	}
}

// linkSecurityGroups draws a group for each security group, plus a stand-in
// group for each prefix list drawPrefixLists drew. Rules that reference a
// drawn group or prefix list target it by ID, which Cloudcraft shows as a
// connection between the groups.
func linkSecurityGroups(r *model.Region, d *diagram) {
	members := sgMembers(r, d)

	// Mark every group as drawn before building rules, so a rule can target
	// a group that appears later in the list.
	for _, n := range r.Of(aws.TypeSecurityGroup) {
		d.drawn[n.ID] = true
	}
	for _, n := range r.Of(aws.TypePrefixList) {
		if d.drawn[n.ID+"-placeholder"] {
			d.drawn[n.ID] = true
		}
	}

	for _, n := range r.Of(aws.TypeSecurityGroup) {
		nodes := members[n.ID]
		if len(nodes) == 0 {
			nodes = []string{n.ID + "-placeholder"}
		}
		d.addGroup(component{
			"id":            n.ID,
			"type":          "sg",
			"region":        d.region,
			"name":          fmt.Sprintf("%s (%s)", n.Name, n.ID),
			"shape":         "dynamic",
			"padding":       1.5,
			"nodes":         nodes,
			"inboundRules":  d.sgRules(n.ID, rules(n, false)),
			"outboundRules": d.sgRules(n.ID, rules(n, true)),
		})
	}

	for _, n := range r.Of(aws.TypePrefixList) {
		if !d.drawn[n.ID] {
			continue
		}
		d.addGroup(component{
			"id":            n.ID,
			"type":          "sg",
			"region":        d.region,
			"name":          fmt.Sprintf("prefix list: %s (%s)", n.Name, n.ID),
			"shape":         "dynamic",
			"padding":       1.5,
			"nodes":         []string{n.ID + "-placeholder"},
			"inboundRules":  []component{},
			"outboundRules": []component{},
		})
	}
}

// targetTypes maps model rule targets to Cloudcraft target types. Prefix
// lists target their stand-in security group.
var targetTypes = map[string]string{
	inventory.TargetCIDR:   "ip",
	inventory.TargetCIDRv6: "ipv6",
	inventory.TargetGroup:  "sg",
	aws.TargetPrefixList:   "sg",
}

// sgRules converts security group rules to Cloudcraft rules.
func (d *diagram) sgRules(sgID string, rules []model.Rule) []component {
	out := []component{}
	for _, rule := range rules {
		ports := portRange(rule)
		switch rule.TargetType {
		case inventory.TargetGroup:
			if !d.drawn[rule.Target] {
				d.warnf(
					"%s: rule %s/%s references %s, which is not in the diagram",
					sgID,
					rule.Protocol,
					ports,
					rule.Target,
				)
				continue
			}
		case aws.TargetPrefixList:
			if !d.drawn[rule.Target] {
				d.warnf(
					"%s: rule %s/%s references prefix list %s, which is not in the diagram",
					sgID,
					rule.Protocol,
					ports,
					rule.Target,
				)
				continue
			}
		}
		out = append(out, component{
			"protocol":    rule.Protocol,
			"portRange":   ports,
			"target":      rule.Target,
			"targetType":  targetTypes[rule.TargetType],
			"description": rule.Description,
			"hidden":      false,
		})
	}
	return out
}

// portRange formats a rule's ports as Cloudcraft exports do: "0" for all
// ports, "443" for one, "1024-65535" for a range.
func portRange(rule model.Rule) string {
	var from, to int32
	if rule.FromPort != nil {
		from = *rule.FromPort
	}
	if rule.ToPort != nil {
		to = *rule.ToPort
	}
	switch {
	case rule.FromPort == nil || from == -1:
		return "0"
	case from == to:
		return fmt.Sprint(from)
	default:
		return fmt.Sprintf("%d-%d", from, to)
	}
}

// linkContainers draws subnet and VPC groups around the nodes placed in
// them. drawContainerPlaceholders has already given every container a member.
func linkContainers(r *model.Region, d *diagram) {
	subnetMembers := map[string][]string{}
	vpcMembers := map[string][]string{}
	for _, p := range d.placements {
		id := p.node["id"].(string)
		if p.subnet != "" {
			subnetMembers[p.subnet] = append(subnetMembers[p.subnet], id)
		}
		if p.vpc != "" {
			vpcMembers[p.vpc] = append(vpcMembers[p.vpc], id)
		}
	}

	for _, n := range r.Of(aws.TypeSubnet) {
		if d.addGroup(component{
			"id":               n.ID,
			"type":             "subnet",
			"region":           d.region,
			"name":             fmt.Sprintf("%s (%s)", n.Name, n.Attrs[aws.AttrCIDR]),
			"shape":            "rectangular",
			"padding":          1.5,
			"public":           n.Tier == inventory.TierPublic,
			"availabilityZone": n.Zone,
			"nodes":            subnetMembers[n.ID],
		}) {
			vpc := vpcOf(r, n)
			vpcMembers[vpc] = append(vpcMembers[vpc], n.ID)
		}
	}

	for _, n := range r.Of(aws.TypeSecurityGroup) {
		if d.drawn[n.ID] {
			vpc := vpcOf(r, n)
			vpcMembers[vpc] = append(vpcMembers[vpc], n.ID)
		}
	}

	for _, n := range r.Of(aws.TypeVPC) {
		d.addGroup(component{
			"id":                 n.ID,
			"type":               "vpc",
			"region":             d.region,
			"name":               fmt.Sprintf("%s (%s)", n.Name, n.Attrs[aws.AttrCIDR]),
			"shape":              "rectangular",
			"padding":            1.5,
			"nodes":              vpcMembers[n.ID],
			"peeringConnections": []string{},
		})
	}
}

// linkVPNConnections draws each VPN connection as an edge from its VPN or
// transit gateway to its customer gateway.
func linkVPNConnections(r *model.Region, d *diagram) {
	for _, e := range r.EdgesOf(aws.RelVPNConnection) {
		if !d.drawn[e.From] || !d.drawn[e.To] {
			d.warnf("skip %s: gateway %q or customer gateway %q is not in the diagram", e.ID, e.From, e.To)
			continue
		}
		d.edges = append(d.edges, component{
			"id":     e.ID,
			"type":   "vpnconnection",
			"from":   e.From,
			"to":     e.To,
			"hidden": false,
		})
	}
}
