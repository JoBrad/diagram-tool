package model

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"diagram/internal/inventory"
)

// The model knows no platform, so these tests use made-up resource types.

// rec returns a record of type rt in parent.
func rec(id, rt, parent string) inventory.Record {
	return inventory.Record{ID: id, Type: rt, Name: id, Parent: parent}
}

// member returns rec with memberships in groups.
func member(r inventory.Record, groups ...string) inventory.Record {
	for _, g := range groups {
		r.Refs = append(r.Refs, inventory.Ref{Relation: inventory.RelMemberOf, Type: "group", ID: g})
	}
	return r
}

// link returns a connection record from one record to another.
func link(id, from, to string) inventory.Record {
	return inventory.Record{ID: id, Type: "link", From: from, To: to, Relation: "link"}
}

// edgeLabel names an edge for comparison. Edges without their own ID are
// named by their ends, since they are kept or dropped with those ends.
func edgeLabel(e *Edge) string {
	if e.ID != "" {
		return e.ID
	}
	return fmt.Sprintf("%s:%s->%s", e.Relation, e.From, e.To)
}

// layout lists each node as "id@parent", then each edge.
func layout(r *Region) []string {
	var out []string
	for _, n := range r.Nodes {
		out = append(out, n.ID+"@"+n.Parent)
	}
	for _, e := range r.Edges {
		out = append(out, edgeLabel(e))
	}
	return out
}

func TestBuild(t *testing.T) {
	server := member(rec("server", "server", "zone"), "group")
	server.Platform, server.Tier, server.Zone = "test", inventory.TierPublic, "a"
	server.Attrs = map[string]string{"size": "large"}
	group := rec("group", "group", "net")
	group.Rules = []inventory.Rule{{Protocol: "tcp", TargetType: inventory.TargetCIDR, Target: "0.0.0.0/0"}}
	stray := rec("stray", "server", "")
	stray.Refs = []inventory.Ref{{Relation: inventory.RelContainedIn, Type: "zone", ID: "zone"}}

	d := Build([]*inventory.Region{{Name: "r1", Records: []inventory.Record{
		rec("net", "network", ""), rec("zone", "zone", "net"), server, group, link("l-1", "server", "far"), stray,
	}}})

	r := d.Regions[0]
	want := []string{
		"net@", "zone@net", "server@zone", "group@net", "stray@",
		"member_of:server->group", "l-1",
	}
	if got := layout(r); !reflect.DeepEqual(got, want) {
		t.Errorf("layout = %v, want %v", got, want)
	}
	n := r.Node("server")
	wantNode := &Node{
		ID: "server", Type: "server", Platform: "test", Name: "server", Parent: "zone", Zone: "a",
		Tier: inventory.TierPublic, Attrs: map[string]string{"size": "large"},
	}
	if !reflect.DeepEqual(n, wantNode) {
		t.Errorf("server = %+v, want %+v", n, wantNode)
	}
	if !reflect.DeepEqual(r.Node("group").Rules, group.Rules) {
		t.Errorf("group rules = %+v", r.Node("group").Rules)
	}
	if e := r.EdgesOf("link"); len(e) != 1 || *e[0] != (Edge{ID: "l-1", Relation: "link", From: "server", To: "far"}) {
		t.Errorf("links = %+v", e)
	}
	if got := r.Of("server"); len(got) != 2 || got[0].ID != "server" || got[1].ID != "stray" {
		t.Errorf("Of(server) = %v", got)
	}
	if r.Node("missing") != nil {
		t.Error("Node(missing) found a node")
	}
}

func TestAncestor(t *testing.T) {
	r := &Region{Nodes: []*Node{
		{ID: "net", Type: "network"},
		{ID: "zone", Type: "zone", Parent: "net"},
		{ID: "server", Type: "server", Parent: "zone"},
	}}
	is := func(rt ResourceType) func(*Node) bool { return func(n *Node) bool { return n.Type == rt } }
	server := r.Node("server")
	if a := r.Ancestor(server, is("network")); a == nil || a.ID != "net" {
		t.Errorf("network of server = %v, want net", a)
	}
	if a := r.Ancestor(server, is("zone")); a == nil || a.ID != "zone" {
		t.Errorf("zone of server = %v, want zone", a)
	}
	if a := r.Ancestor(r.Node("net"), is("network")); a != nil {
		t.Errorf("network above net = %v, want none", a)
	}
}

// TestBuildHidesBeforePruning checks that hiding servers leaves their zone
// empty, so pruning drops it.
func TestBuildHidesBeforePruning(t *testing.T) {
	server := rec("server", "server", "zone")
	server.Hidden = true
	zone := rec("zone", "zone", "")
	zone.Prunable = true
	d := Build([]*inventory.Region{{Name: "r1", Records: []inventory.Record{zone, server}}})
	if got := layout(d.Regions[0]); len(got) != 0 {
		t.Errorf("kept %v; want nothing once the server is hidden", got)
	}
}

// TestBuildLeavesInputAlone checks that Build does not change the records it
// was given.
func TestBuildLeavesInputAlone(t *testing.T) {
	inst := member(rec("server", "server", "zone"), "group")
	zone := rec("zone", "zone", "")
	zone.Hidden = true
	group := rec("group", "group", "")
	group.Hidden = true
	records := []inventory.Record{zone, inst, group}
	before := slices.Clone(records)
	beforeRefs := slices.Clone(inst.Refs)
	Build([]*inventory.Region{{Name: "r1", Records: records}})
	if !reflect.DeepEqual(records, before) || !reflect.DeepEqual(records[1].Refs, beforeRefs) {
		t.Errorf("records changed: %+v", records)
	}
}
