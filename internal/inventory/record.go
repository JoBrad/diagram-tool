package inventory

// Record is one resource in a form that does not depend on where it was
// collected, or on which platform it belongs to. A platform package fills in
// what its resources mean (containment, connections, selection), so the model
// can draw any platform's records without knowing the platform.
type Record struct {
	ID string
	// Type is the resource type, such as "AWS::EC2::Instance". The model
	// treats it as opaque; renderers use it, with Platform, to pick a shape.
	Type string
	// Platform names the platform the resource belongs to, such as "aws".
	Platform string
	// Name is the display name: the Name tag, a resource's own name, or the
	// ID.
	Name string

	// Parent is the ID of the record that contains this one, such as a
	// subnet or VPC, or "" for a resource that sits in the region itself.
	Parent string
	// Zone is the availability zone a container spans, when it has one.
	Zone string
	// Tier is how exposed the resource is, for containers and resources that
	// know it.
	Tier Tier

	// From and To are set when the record is a connection between two other
	// records, such as a VPN connection. Relation names the connection.
	From, To string
	Relation Relation

	// Refs are references to other records. After the platform has finished
	// a region, only memberships remain; before that, platforms also use
	// RelContainedIn references to work out Parent.
	Refs []Ref

	// Selection. KeepIf lists records that keep this one when unconnected
	// resources are pruned: it is kept while any of them is. A kept record
	// keeps its parent too, unless it is Attached: hung on its parent, such
	// as an internet gateway on a VPC, rather than held inside it. Prunable
	// marks a record that pruning may drop. The zero value of each keeps
	// resources drawn. Hidden and HideChildren carry the config's hide
	// settings.
	KeepIf       []string
	Attached     bool
	Prunable     bool
	Hidden       bool
	HideChildren bool

	// Attrs holds the resource type's scalar attributes, such as
	// "instance_type", for renderers' labels and shapes.
	Attrs map[string]string
	// Rules holds a security group's rules, inbound and outbound.
	Rules []Rule
}

// Relation is how a record refers to another resource.
type Relation string

// Record relations.
const (
	// RelContainedIn points at a container a resource may sit in. Platforms
	// resolve these to Parent; the model never sees them.
	RelContainedIn Relation = "contained_in"
	// RelMemberOf points at a group the resource is in, such as a security
	// group.
	RelMemberOf Relation = "member_of"
)

// Ref is one reference from a record to another resource. Type is the
// target's resource type, so the reference can be read without the target's record.
type Ref struct {
	Relation Relation
	Type     string
	ID       string
}

// Tier is how exposed a resource is.
type Tier string

// Tiers, from most to least exposed. An empty tier is unknown.
const (
	TierPublic   Tier = "public"
	TierPrivate  Tier = "private"
	TierIsolated Tier = "isolated"
)

// Tiers lists the tiers from most to least exposed.
var Tiers = []Tier{TierPublic, TierPrivate, TierIsolated}

// Rule is one security group rule with one target.
type Rule struct {
	Egress bool
	// Deny marks a rule that blocks the traffic it matches. A platform
	// whose rules are evaluated in order, such as Azure's, lists them in
	// that order. The zero value allows, as every AWS rule does.
	Deny bool
	// Protocol is the IP protocol; "-1" means all.
	Protocol string
	// FromPort and ToPort are nil when the rule covers every port.
	FromPort, ToPort *int32
	// TargetType is what the rule allows traffic to or from: one of the
	// Target constants, or a platform's own target type.
	TargetType string
	// Target is a CIDR, a group ID, or a platform-specific ID.
	Target      string
	Description string
}

// Rule target types every platform can use.
const (
	TargetCIDR   = "cidr"
	TargetCIDRv6 = "cidr6"
	TargetGroup  = "security_group"
)
