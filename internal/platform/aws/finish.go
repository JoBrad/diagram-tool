package aws

import (
	"cmp"
	"slices"

	"diagram/internal/config"
	"diagram/internal/inventory"
)

// kind is how Finish applies the config to one resource type.
type kind struct {
	// query returns the config's query for the type, or nil.
	query func(*config.Resources) *config.ResourceQuery
	// prunable types are dropped when unconnected and the config asks for
	// that. Instances are never pruned: they are what the config asks to
	// draw.
	prunable bool
}

// kinds lists the resource types the config can hide or prune. Any other
// type, such as one read through include_all, is always kept.
var kinds = map[string]kind{
	TypeInstance: {config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.Instances }), false},
	TypeVPNConnection: {
		config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.VPNConnections }), true,
	},
	TypeVPC:    {config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.VPCs }), true},
	TypeSubnet: {config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.Subnets }), true},
	TypeSecurityGroup: {
		config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.SecurityGroups }), true,
	},
	TypeNATGateway: {
		config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.NATGateways }),
		true,
	},
	TypeInternetGateway: {
		config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.InternetGateways }), true,
	},
	TypeVPNGateway: {
		config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.VPNGateways }),
		true,
	},
	TypeTransitGateway: {
		config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.TransitGateways }), true,
	},
	TypeCustomerGateway: {
		config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.CustomerGateways }), true,
	},
	TypePrefixList: {
		config.EC2Query(func(q *config.EC2Resources) *config.ResourceQuery { return q.PrefixLists }),
		true,
	},
}

// Finish completes the records a source read, region by region: it sets each
// record's Parent, says what keeps each one when unconnected resources are
// pruned, and applies the config's hide and show_unconnected_resources
// settings. The model reads only what Finish sets.
func Finish(cfg *config.Config, regions []*inventory.Region) {
	_, acct := cfg.Account()
	for _, r := range regions {
		vpcOf := resolveParents(r)
		connect(r, vpcOf)
		applyConfig(r.Records, &acct.Resources, cfg.Options.ShowUnconnectedResources)
	}
}

// resolveParents sets each record's Parent from its containment references:
// its subnet when that is in the region, else its VPC when that is, else
// none. It returns each record's VPC as its references named it, whether or
// not the VPC is in the region, for connect.
//
// A resource listed in several subnets is drawn once, in the first by
// availability zone and then by ID. Sorting by zone puts a resource that
// spans zones in the same zone as its peers, whatever order its source
// listed them in. A subnet the region does not hold sorts last, as its zone
// is unknown.
func resolveParents(r *inventory.Region) map[string]string {
	zone := map[string]string{}
	present := map[string]bool{}
	for _, rec := range r.Records {
		present[rec.ID] = true
		if rec.Type == TypeSubnet {
			zone[rec.ID] = rec.Zone
		}
	}

	vpcOf := map[string]string{}
	for i := range r.Records {
		rec := &r.Records[i]
		var subnets []string
		for _, ref := range rec.Refs {
			if ref.Relation != inventory.RelContainedIn {
				continue
			}
			switch ref.Type {
			case TypeSubnet:
				subnets = append(subnets, ref.ID)
			case TypeVPC:
				vpcOf[rec.ID] = ref.ID
			}
		}
		rec.Parent = ""
		if len(subnets) > 0 {
			first := slices.MinFunc(subnets, func(a, b string) int {
				za, zb := zone[a], zone[b]
				if (za == "") != (zb == "") {
					return cmp.Compare(boolInt(za == ""), boolInt(zb == ""))
				}
				return cmp.Or(cmp.Compare(za, zb), cmp.Compare(a, b))
			})
			if present[first] {
				rec.Parent = first
			}
		}
		if vpc := vpcOf[rec.ID]; rec.Parent == "" && present[vpc] {
			rec.Parent = vpc
		}
		rec.Refs = slices.DeleteFunc(rec.Refs, func(ref inventory.Ref) bool {
			return ref.Relation == inventory.RelContainedIn
		})
	}
	return vpcOf
}

// boolInt is 1 for true, so false sorts first.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// connect says what keeps each record when unconnected resources are pruned.
// A container is kept by what it holds, which the model reads from Parent;
// KeepIf adds what else connects a resource.
//
// A gateway attached to a VPC does not keep the VPC: every default VPC has
// an internet gateway, and the two would otherwise keep each other. A VPN
// gateway keeps its VPC only while a kept VPN connection uses it. Without
// route tables there is no record of which instances use a NAT gateway, so
// a NAT gateway is kept while its VPC holds a kept instance.
func connect(r *inventory.Region, vpcOf map[string]string) {
	instancesIn := map[string][]string{} // VPC -> its instances
	vgwsIn := map[string][]string{}      // VPC -> its VPN gateways
	members := map[string][]string{}     // security group -> its members
	vpnsFrom := map[string][]string{}    // gateway -> its VPN connections
	vpnsTo := map[string][]string{}      // customer gateway -> its VPN connections
	referrers := map[string][]string{}   // prefix list -> security groups whose rules name it
	for _, rec := range r.Records {
		switch rec.Type {
		case TypeInstance:
			instancesIn[vpcOf[rec.ID]] = append(instancesIn[vpcOf[rec.ID]], rec.ID)
		case TypeVPNGateway:
			vgwsIn[vpcOf[rec.ID]] = append(vgwsIn[vpcOf[rec.ID]], rec.ID)
		case TypeVPNConnection:
			vpnsFrom[rec.From] = append(vpnsFrom[rec.From], rec.ID)
			vpnsTo[rec.To] = append(vpnsTo[rec.To], rec.ID)
		case TypeSecurityGroup:
			for _, rule := range rec.Rules {
				if rule.TargetType == TargetPrefixList {
					referrers[rule.Target] = append(referrers[rule.Target], rec.ID)
				}
			}
		}
		for _, ref := range rec.Refs {
			if ref.Relation == inventory.RelMemberOf {
				members[ref.ID] = append(members[ref.ID], rec.ID)
			}
		}
	}

	vpcRef := func(id string) []string {
		if vpc := vpcOf[id]; vpc != "" {
			return []string{vpc}
		}
		return nil
	}
	for i := range r.Records {
		rec := &r.Records[i]
		switch rec.Type {
		case TypeVPC:
			for _, vgw := range vgwsIn[rec.ID] {
				rec.KeepIf = append(rec.KeepIf, vpnsFrom[vgw]...)
			}
		case TypeInternetGateway:
			rec.KeepIf, rec.Attached = vpcRef(rec.ID), true
		case TypeVPNGateway:
			rec.KeepIf, rec.Attached = append(vpcRef(rec.ID), vpnsFrom[rec.ID]...), true
		case TypeNATGateway:
			rec.KeepIf = instancesIn[vpcOf[rec.ID]]
		case TypeSecurityGroup:
			rec.KeepIf = members[rec.ID]
		case TypePrefixList:
			rec.KeepIf = referrers[rec.ID]
		case TypeTransitGateway:
			rec.KeepIf = vpnsFrom[rec.ID]
		case TypeCustomerGateway:
			rec.KeepIf = vpnsTo[rec.ID]
		}
	}
}

// applyConfig sets each record's hide flags from its type's query, and marks
// it prunable when its type can be pruned and its show_unconnected_resources
// setting is false: the type's own value, else the global option, else true.
// Listed IDs mean "draw these", so a type queried by ID is never pruned;
// config.Read rejects a query that sets both.
func applyConfig(records []inventory.Record, res *config.Resources, global *bool) {
	show := func(q *config.ResourceQuery) bool {
		if q != nil && len(q.IDs) > 0 {
			return true
		}
		if q != nil && q.ShowUnconnectedResources != nil {
			return *q.ShowUnconnectedResources
		}
		if global != nil {
			return *global
		}
		return true
	}
	for i := range records {
		rec := &records[i]
		k, ok := kinds[rec.Type]
		if !ok {
			continue
		}
		q := k.query(res)
		if q != nil {
			rec.Hidden, rec.HideChildren = q.Hide, q.HideChildren
		}
		rec.Prunable = k.prunable && !show(q)
	}
}
