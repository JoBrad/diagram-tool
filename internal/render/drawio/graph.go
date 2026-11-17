package drawio

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/layout"
	"diagram/internal/model"
	"diagram/internal/platform/aws"
	"diagram/internal/platform/azure"
)

// graph turns the diagram into a layout graph and the cells to draw.
// Regions, VPCs, subnets, and security group boxes are containers.
//
// The canvas reads left to right from most to least contained, and top to
// bottom from public to private:
//   - A region is one row: its VPCs, then what sits in the region itself
//     (Lambda functions outside any VPC, prefix lists, customer gateways).
//   - A VPC is one row: its subnets, then what sits in the VPC itself.
//   - Subnets form a grid with Availability Zones as columns, public subnets
//     in the rows above private ones.
//   - Within a container, public resources (those with a public IP) and the
//     security group boxes holding them come first.
//
// The groupings that are not AWS containers (a region's or VPC's loose
// resources, the subnet grid) are layout-only blocks: laid out, never drawn,
// so their contents are drawn directly in the region or VPC.
func graph(m *model.Diagram, colors config.Colors) (layout.Graph, []cell) {
	b := builder{colors: newPalette(colors)}
	for _, r := range m.Regions {
		b.region(r)
	}
	return b.g, b.cells
}

// builder collects the layout graph and the cells as regions are added.
type builder struct {
	g     layout.Graph
	cells []cell
	// blocks maps a drawn container to the layout-only block that holds its
	// loose resources, for the region being added.
	blocks map[string]string
	colors *palette
}

// add lays out it and draws c. A cell drawn under a different parent than its
// layout parent sets c.parent; otherwise it is drawn in the layout parent.
func (b *builder) add(it layout.Item, c cell) {
	b.g.Items = append(b.g.Items, it)
	c.id, c.parent = it.ID, cmp.Or(c.parent, it.Parent)
	b.cells = append(b.cells, c)
}

// addBlock adds a layout-only container.
func (b *builder) addBlock(id, parent string, columns int) {
	b.g.Items = append(b.g.Items, layout.Item{ID: id, Parent: parent, Container: true, Columns: columns})
}

// place adds an item that may be a loose resource of a region or VPC: it is
// laid out in that container's block and drawn in the container itself.
func (b *builder) place(it layout.Item, c cell) {
	if block, ok := b.blocks[it.Parent]; ok {
		it.Parent, c.parent = block, it.Parent
	}
	b.add(it, c)
}

func (b *builder) region(r *model.Region) {
	// present holds what is drawn; leaves and edges check it.
	present := map[string]bool{r.Name: true}
	for _, n := range r.Nodes {
		if isNetwork(n) {
			present[n.ID] = true
		}
	}
	home := leafHomes(r)
	b.blocks = map[string]string{}
	regionAt := len(b.g.Items)
	style := regionStyle
	if platformOf(r) == azure.Platform {
		style = azureRegionStyle
	}
	b.add(layout.Item{ID: r.Name, Label: r.Name, Container: true},
		cell{label: r.Name, style: style, data: [][2]string{{"type", "region"}}})
	b.networks(r, home)
	if len(r.Of(aws.TypePrefixList)) > 0 || slices.Contains(slices.Collect(maps.Values(home)), r.Name) {
		b.blocks[r.Name] = r.Name + "-regional"
		b.addBlock(r.Name+"-regional", r.Name, 0)
	}
	// The region is one row of whatever it directly holds.
	for _, it := range b.g.Items[regionAt+1:] {
		if it.Parent == r.Name {
			b.g.Items[regionAt].Columns++
		}
	}

	boxes, inBox := sgBoxes(r, home)
	slices.SortStableFunc(boxes, func(x, y *sgBox) int { return publicFirst(x.public(r), y.public(r)) })
	for _, box := range boxes {
		style := sgStyle
		if c := b.boxColor(box); c != "" {
			style = sgStyleFor(c)
		}
		b.place(layout.Item{ID: box.id, Parent: box.parent, Label: box.label(), Container: true},
			cell{label: box.label(), style: style, data: box.data()})
	}

	groups := securityGroups(r)
	leaves := slices.Clone(r.Nodes)
	slices.SortStableFunc(leaves, func(x, y *model.Node) int { return publicFirst(isPublic(x), isPublic(y)) })
	for _, n := range leaves {
		style, icon, ok := leafStyle(n)
		if !ok {
			continue
		}
		label := leafLabel(n)
		data := nodeData(n)
		if sgs := groups[n.ID]; len(sgs) > 0 {
			data = append(data, [2]string{"securityGroups", strings.Join(sgs, ", ")})
		}
		b.place(
			layout.Item{
				ID: n.ID, Parent: cmp.Or(inBox[n.ID], home[n.ID]), Label: label, Width: slotWidth, Height: slotHeight,
			},
			cell{label: leafCellLabel(label), style: style, data: data, icon: icon},
		)
		present[n.ID] = true
	}
	for _, n := range r.Of(aws.TypePrefixList) {
		label := "prefix list: " + n.Name
		b.place(layout.Item{ID: n.ID, Parent: r.Name, Label: label, Width: 160, Height: 50},
			cell{label: label, style: prefixListStyle, data: nodeData(n)})
		present[n.ID] = true
	}

	links, edges := edgeCells(r, present, boxes, b.origin(boxes, home))
	b.g.Links = append(b.g.Links, links...)
	b.cells = append(b.cells, edges...)
}

// leafHomes returns the container each drawn leaf sits in: its subnet, else
// its VPC, else the region.
func leafHomes(r *model.Region) map[string]string {
	home := map[string]string{}
	for _, n := range r.Nodes {
		if _, _, ok := leafStyle(n); ok {
			home[n.ID] = networkOf(r, n, isNetwork)
		}
	}
	return home
}

// isNetwork reports whether n is a VPC or subnet, the containers drawn as
// boxes that hold resources.
func isNetwork(n *model.Node) bool {
	return isVPC(n) || isSubnet(n)
}

// isVPC reports whether n is a VPC or its counterpart, an Azure virtual
// network.
func isVPC(n *model.Node) bool {
	return n.Type == aws.TypeVPC || n.Type == azure.TypeVirtualNetwork
}

// isSubnet reports whether n is an AWS or Azure subnet.
func isSubnet(n *model.Node) bool {
	return n.Type == aws.TypeSubnet || n.Type == azure.TypeSubnet
}

// isSecurityGroup reports whether n is an AWS security group or its
// counterpart, an Azure network security group. Both are drawn as boxes
// around their members.
func isSecurityGroup(n *model.Node) bool {
	return n.Type == aws.TypeSecurityGroup || n.Type == azure.TypeNetworkSecurityGroup
}

// ofKind returns the region's nodes that match, in order.
func ofKind(r *model.Region, match func(*model.Node) bool) []*model.Node {
	var nodes []*model.Node
	for _, n := range r.Nodes {
		if match(n) {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// platformOf returns the platform of the region's resources. A diagram
// covers one account for now, so the first node's platform is every node's.
func platformOf(r *model.Region) string {
	if len(r.Nodes) == 0 {
		return ""
	}
	return r.Nodes[0].Platform
}

// cidrLabel names a VPC or subnet with its address ranges.
func cidrLabel(n *model.Node) string {
	return fmt.Sprintf("%s (%s)", n.Name, cidrOf(n))
}

// cidrCellLabel is cidrLabel as an html=1 label, the ranges in the mono font.
func cidrCellLabel(n *model.Node) string {
	return n.Name + " (" + mono(cidrOf(n)) + ")"
}

// cidrOf returns a VPC's or subnet's address ranges.
func cidrOf(n *model.Node) string {
	if n.Platform == azure.Platform {
		return strings.ReplaceAll(n.Attrs[azure.AttrCIDR], ",", ", ")
	}
	return n.Attrs[aws.AttrCIDR]
}

// networkOf returns the nearest container above n that match accepts, or
// the region when there is none.
func networkOf(r *model.Region, n *model.Node, match func(*model.Node) bool) string {
	if c := r.Ancestor(n, match); c != nil {
		return c.ID
	}
	return r.Name
}

// subnetTier is a subnet's tier, private when its source did not say.
func subnetTier(n *model.Node) inventory.Tier {
	return cmp.Or(n.Tier, inventory.TierPrivate)
}

// networks adds the region's VPCs and subnets. A VPC with drawn subnets is
// one row: the subnet grid, then a block for its loose resources when it has
// any. A VPC without subnets holds its resources directly.
func (b *builder) networks(r *model.Region, home map[string]string) {
	subnets := map[string][]*model.Node{} // container -> its subnets
	for _, n := range ofKind(r, isSubnet) {
		parent := networkOf(r, n, isVPC)
		subnets[parent] = append(subnets[parent], n)
	}
	loose := map[string]bool{}
	for _, h := range home {
		loose[h] = true
	}

	for _, n := range ofKind(r, isVPC) {
		label := cidrLabel(n)
		it := layout.Item{ID: n.ID, Parent: r.Name, Label: label, Container: true}
		if len(subnets[n.ID]) > 0 && loose[n.ID] {
			it.Columns = 2
		}
		style := vpcStyleFor(n, b.colors.resources)
		b.add(it, cell{label: cidrCellLabel(n), style: style, data: nodeData(n)})
		if len(subnets[n.ID]) == 0 {
			continue
		}
		b.subnetGrid(n.ID, subnets[n.ID])
		if loose[n.ID] {
			b.blocks[n.ID] = n.ID + "-resources"
			b.addBlock(n.ID+"-resources", n.ID, 0)
		}
	}
	// Subnets whose VPC is hidden sit in the region, in their own grid.
	if len(subnets[r.Name]) > 0 {
		b.subnetGrid(r.Name, subnets[r.Name])
	}
}

// subnetGrid lays a container's subnets out with Availability Zones as
// columns: a band of public rows above a band of private ones. A band has as
// many rows as its busiest zone needs, and invisible spacers fill the rest,
// so each zone stays in its column.
func (b *builder) subnetGrid(container string, subnets []*model.Node) {
	var zones []string
	for _, n := range subnets {
		if z := n.Zone; !slices.Contains(zones, z) {
			zones = append(zones, z)
		}
	}
	slices.Sort(zones)
	grid := container + "-subnets"
	b.addBlock(grid, container, len(zones))

	for _, tier := range inventory.Tiers {
		cells := map[string][]*model.Node{}
		rows := 0
		for _, n := range subnets {
			if subnetTier(n) == tier {
				cells[n.Zone] = append(cells[n.Zone], n)
				rows = max(rows, len(cells[n.Zone]))
			}
		}
		for _, z := range zones {
			slices.SortFunc(
				cells[z],
				func(x, y *model.Node) int { return cmp.Or(cmp.Compare(x.Name, y.Name), cmp.Compare(x.ID, y.ID)) },
			)
		}
		for row := range rows {
			for col, z := range zones {
				if row >= len(cells[z]) {
					id := fmt.Sprintf("%s-gap-%s-%d-%s", grid, tier, row, z)
					b.g.Items = append(b.g.Items, layout.Item{ID: id, Parent: grid, Width: 1, Height: 1})
					continue
				}
				n := cells[z][row]
				label := cidrLabel(n)
				// Neighbors alternate tints in a checkerboard, across zones and rows.
				color, alt := b.colors.subnet(n), (row+col)%2 == 1
				style := subnetStyle(color, alt)
				if n.Platform == azure.Platform {
					style = azureSubnetStyle(color, alt)
				}
				b.add(layout.Item{ID: n.ID, Parent: grid, Label: label, Container: true},
					cell{parent: container, label: cidrCellLabel(n), style: style, data: nodeData(n)})
			}
		}
	}
}

// isPublic reports whether a resource is internet-facing, such as an
// instance with a public IP.
func isPublic(n *model.Node) bool {
	return n.Tier == inventory.TierPublic && !isNetwork(n)
}

// public reports whether any of the box's members is public. A box nested in
// another shares its members, so an outer box is public whenever an inner one
// is, and sorting by this keeps parents ahead of their children.
func (b *sgBox) public(r *model.Region) bool {
	return slices.ContainsFunc(r.Nodes, func(n *model.Node) bool {
		return isPublic(n) && slices.Contains(b.members, n.ID)
	})
}

// publicFirst orders public before private.
func publicFirst(x, y bool) int {
	switch {
	case x == y:
		return 0
	case x:
		return -1
	}
	return 1
}

// boxColor returns a security group box's override color, from the first of
// its groups that has one, or "".
func (b *builder) boxColor(box *sgBox) string {
	for _, g := range box.groups {
		if c, ok := b.colors.resources[g.ID]; ok {
			return c
		}
	}
	return ""
}

// origin returns the color for a rule edge that starts at id: the start's own
// override, else the color of the container it sits in (for a security group
// box, the subnet or VPC it is drawn in), else the default rule color.
func (b *builder) origin(boxes []*sgBox, home map[string]string) func(id string) string {
	byID := map[string]*sgBox{}
	for _, box := range boxes {
		byID[box.id] = box
	}
	return func(id string) string {
		if c, ok := b.colors.resources[id]; ok {
			return c
		}
		container := home[id]
		if box, ok := byID[id]; ok {
			if c := b.boxColor(box); c != "" {
				return c
			}
			container = box.container
		}
		return cmp.Or(b.colors.container(container), ruleColor)
	}
}
