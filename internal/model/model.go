// Package model is the tool-neutral diagram: which resources to draw, where
// each one lives, and how they connect. Build fills it from inventory
// records, hiding and pruning as the records say; renderers read it. It knows
// nothing about any platform or drawing tool: a platform package has already
// set what each record means (its parent, what keeps it, whether the config
// hides it).
//
// IDs are the platform's resource IDs, so every reference is a plain ID.
package model

import (
	"log"

	"diagram/internal/inventory"
)

// ResourceType is a resource type, such as "AWS::EC2::Instance". The model
// does not interpret it.
type ResourceType string

// Rule is one security group rule with one target.
type Rule = inventory.Rule

// Node is one resource. Within a region, nodes keep the order inventory
// returned them in, so a renderer's output is reproducible.
type Node struct {
	ID       string
	Type     ResourceType
	Platform string
	// Name is the display name: the Name tag, a resource's own name field,
	// or the ID.
	Name string
	// Parent is the ID of the node this one sits in, or "" for one that sits
	// in the region. A parent is always a node of the same region.
	Parent string
	Zone   string
	Tier   inventory.Tier
	// Attrs holds the resource type's attributes, such as "instance_type".
	Attrs map[string]string
	// Rules holds a security group's rules, inbound and outbound.
	Rules []Rule
}

// Relation is a relationship type.
type Relation string

// RelMemberOf runs from a resource to a group it is in, such as a security
// group.
const RelMemberOf = Relation(inventory.RelMemberOf)

// Edge is a relationship between two nodes.
type Edge struct {
	// ID is the edge's own resource ID when it has one, such as a VPN
	// connection.
	ID       string
	Relation Relation
	From     string
	To       string
}

// Region holds the resources drawn for one region.
type Region struct {
	Name  string
	Nodes []*Node
	Edges []*Edge
	// byID indexes Nodes for Node; it is built on first use.
	byID map[string]*Node
}

// Diagram holds every region.
type Diagram struct {
	Regions []*Region
}

// Build returns the diagram for the collected regions. Hiding runs before
// pruning, so pruning sees the result: hiding instances can leave their
// subnets empty.
func Build(regions []*inventory.Region) *Diagram {
	d := &Diagram{}
	for _, inv := range regions {
		r := &Region{Name: inv.Name}
		records := hide(inv.Records)
		records = prune(records)
		r.add(records)
		d.Regions = append(d.Regions, r)
	}
	return d
}

// Of returns the region's nodes of one resource type, in order.
func (r *Region) Of(rt ResourceType) []*Node {
	var nodes []*Node
	for _, n := range r.Nodes {
		if n.Type == rt {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// EdgesOf returns the region's edges with one relation, in order.
func (r *Region) EdgesOf(rel Relation) []*Edge {
	var edges []*Edge
	for _, e := range r.Edges {
		if e.Relation == rel {
			edges = append(edges, e)
		}
	}
	return edges
}

// Node returns the region's node with the given ID, or nil.
func (r *Region) Node(id string) *Node {
	if r.byID == nil {
		r.byID = make(map[string]*Node, len(r.Nodes))
		for _, n := range r.Nodes {
			r.byID[n.ID] = n
		}
	}
	return r.byID[id]
}

// Ancestor returns the nearest node above n, starting with its parent, for
// which match is true, or nil.
func (r *Region) Ancestor(n *Node, match func(*Node) bool) *Node {
	for p := r.Node(n.Parent); p != nil; p = r.Node(p.Parent) {
		if match(p) {
			return p
		}
	}
	return nil
}

// warnf reports something left out of the diagram, so it surfaces for triage
// instead of disappearing.
func (r *Region) warnf(format string, args ...any) {
	log.Printf("WARN %s: "+format, append([]any{r.Name}, args...)...)
}
