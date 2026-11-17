package drawio

import (
	"reflect"
	"testing"

	"diagram/internal/inventory"
	"diagram/internal/model"
	"diagram/internal/platform/aws"
)

func TestRuleLinks(t *testing.T) {
	tcp := func(port int32, rt, target string) model.Rule {
		return model.Rule{Protocol: "tcp", FromPort: &port, ToPort: &port, TargetType: rt, Target: target}
	}
	sg := func(id string, inbound ...model.Rule) *model.Node {
		return &model.Node{ID: id, Type: aws.TypeSecurityGroup, Name: id, Rules: inbound}
	}
	r := &model.Region{Name: "us-east-1", Nodes: []*model.Node{
		// sg-db spans two subnets and admits sg-app, sg-edge (no box), a
		// prefix list, and an undrawn group.
		sg("sg-db",
			tcp(5432, inventory.TargetGroup, "sg-app"),
			tcp(5432, inventory.TargetGroup, "sg-app"), // duplicate port, drawn once
			tcp(22, inventory.TargetGroup, "sg-edge"),
			tcp(443, aws.TargetPrefixList, "pl-1"),
			tcp(80, inventory.TargetGroup, "sg-gone"),
			tcp(1, inventory.TargetGroup, "sg-db"), // self-reference
			model.Rule{Protocol: "-1", TargetType: inventory.TargetCIDR, Target: "10.0.0.0/8"},
			// Outbound rules draw nothing.
			model.Rule{Egress: true, Protocol: "tcp", TargetType: inventory.TargetGroup, Target: "sg-edge"},
		),
		sg("sg-app"),
		sg("sg-edge"),
		// sg-pair shares one box with sg-db in subnet-b, so its rule from
		// sg-db stays inside that box and draws nothing there.
		sg("sg-pair", tcp(9000, inventory.TargetGroup, "sg-db")),
	}, Edges: []*model.Edge{
		{Relation: model.RelMemberOf, From: "fn-1", To: "sg-edge"},
	}}
	box := func(id, container string, groups ...string) *sgBox {
		b := &sgBox{id: id, container: container}
		for _, g := range groups {
			b.groups = append(b.groups, &model.Node{ID: g})
		}
		return b
	}
	boxes := []*sgBox{
		box("db-a", "subnet-a", "sg-db"),
		box("db-b", "subnet-b", "sg-db", "sg-pair"),
		box("app-b", "subnet-b", "sg-app"),
	}
	present := map[string]bool{"pl-1": true, "fn-1": true}

	got := map[string]string{}
	for _, l := range ruleLinks(r, present, boxes) {
		got[l.From+">"+l.To] = l.Label
	}
	want := map[string]string{
		// Each sg-db box gets an edge. subnet-b's comes from sg-app's box in
		// the same subnet; subnet-a has none, so it uses sg-app's first box.
		"app-b>db-a": "tcp/5432",
		"app-b>db-b": "tcp/5432",
		// A group with no box is stood for by its first drawn member.
		"fn-1>db-a": "tcp/22",
		"fn-1>db-b": "tcp/22",
		"pl-1>db-a": "tcp/443",
		"pl-1>db-b": "tcp/443",
		// sg-pair's only box is db-b. Its rule from sg-db picks sg-db's box in
		// the same subnet, which is db-b itself, so nothing is drawn.
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("links = %v, want %v", got, want)
	}
}
