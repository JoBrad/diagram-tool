package cloudcraft

import (
	"slices"
)

// Layout spacing, in Cloudcraft grid tiles.
const (
	colSpacing  = 4.0
	rowSpacing  = 4.0
	vpcGap      = 4.0
	gatewayGap  = 3.0
	externalGap = 6.0
	regionGap   = 8.0
)

// tier is the horizontal band a node is drawn in.
type tier int

const (
	// tierCompute holds nodes inside subnets, one column per subnet.
	tierCompute tier = iota
	// tierGateway holds gateways, under their VPC when attached to one.
	tierGateway
	// tierExternal holds nodes outside AWS, such as customer gateways.
	tierExternal
)

// placement says where a node belongs. Layout turns it into a position.
type placement struct {
	node   component
	vpc    string
	subnet string
	tier   tier
}

// layout sets mapPos on every node, starting at offsetX, and returns the
// width used.
//
// Each VPC is a block of subnet columns, with its compute nodes stacked in
// their subnet's column and its gateways in a row underneath. Gateways with
// no VPC follow the VPC blocks on the gateway row. External nodes sit in one
// row below everything.
func (d *diagram) layout(offsetX float64) float64 {
	var vpcs []string
	subnetsByVPC := map[string][]string{}
	// Keyed by VPC and subnet, because nodes placed directly in a VPC all
	// share the empty subnet.
	computeBySubnet := map[[2]string][]placement{}
	gatewaysByVPC := map[string][]placement{}
	var external []placement
	maxRows := 1

	for _, p := range d.placements {
		if p.vpc != "" && !slices.Contains(vpcs, p.vpc) {
			vpcs = append(vpcs, p.vpc)
		}
		switch p.tier {
		case tierCompute:
			if !slices.Contains(subnetsByVPC[p.vpc], p.subnet) {
				subnetsByVPC[p.vpc] = append(subnetsByVPC[p.vpc], p.subnet)
			}
			key := [2]string{p.vpc, p.subnet}
			computeBySubnet[key] = append(computeBySubnet[key], p)
			maxRows = max(maxRows, len(computeBySubnet[key]))
		case tierGateway:
			gatewaysByVPC[p.vpc] = append(gatewaysByVPC[p.vpc], p)
		case tierExternal:
			external = append(external, p)
		}
	}
	slices.Sort(vpcs)

	gatewayY := float64(maxRows-1)*rowSpacing + gatewayGap
	externalY := gatewayY + externalGap

	x := offsetX
	for _, vpc := range vpcs {
		subnets := subnetsByVPC[vpc]
		slices.Sort(subnets)
		for col, subnet := range subnets {
			for row, p := range computeBySubnet[[2]string{vpc, subnet}] {
				p.node["mapPos"] = []float64{x + float64(col)*colSpacing, float64(row) * rowSpacing}
			}
		}
		for col, p := range gatewaysByVPC[vpc] {
			p.node["mapPos"] = []float64{x + float64(col)*colSpacing, gatewayY}
		}
		cols := max(len(subnets), len(gatewaysByVPC[vpc]), 1)
		x += float64(cols)*colSpacing + vpcGap
	}
	for col, p := range gatewaysByVPC[""] {
		p.node["mapPos"] = []float64{x + float64(col)*colSpacing, gatewayY}
	}
	x += float64(len(gatewaysByVPC[""])) * colSpacing

	for col, p := range external {
		p.node["mapPos"] = []float64{offsetX + float64(col)*colSpacing, externalY}
	}
	width := max(x, offsetX+float64(len(external))*colSpacing) - offsetX
	return width
}
