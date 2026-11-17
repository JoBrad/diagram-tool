package drawio

import (
	"context"
	"encoding/xml"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/layout"
	"diagram/internal/model"
	"diagram/internal/platform/aws"
)

// gridModel is one VPC with subnets in two zones, public and private, a
// gateway loose in the VPC, and a public and a private instance sharing a
// private subnet.
func gridModel() *model.Diagram {
	subnet := func(id, zone string, public bool) *model.Node {
		n := &model.Node{ID: id, Type: aws.TypeSubnet, Name: id, Parent: "vpc-1", Zone: zone,
			Tier: inventory.TierPrivate, Attrs: map[string]string{"cidr": "10.0.0.0/24"}}
		if public {
			n.Tier = inventory.TierPublic
		}
		return n
	}
	inst := func(id, publicIP string) *model.Node {
		n := &model.Node{ID: id, Type: aws.TypeInstance, Name: id, Parent: "priv-a",
			Attrs: map[string]string{"instance_type": "t3.micro", "public_ip": publicIP}}
		if publicIP != "" {
			n.Tier = inventory.TierPublic
		}
		return n
	}
	return &model.Diagram{Regions: []*model.Region{{Name: "us-east-1", Nodes: []*model.Node{
		{ID: "vpc-1", Type: aws.TypeVPC, Name: "vpc", Attrs: map[string]string{"cidr": "10.0.0.0/16"}},
		// Listed private first and zone b first; the grid reorders them.
		subnet("priv-b", "us-east-1b", false),
		subnet("priv-a", "us-east-1a", false),
		subnet("priv-a2", "us-east-1a", false),
		subnet("pub-a", "us-east-1a", true),
		inst("i-private", ""),
		inst("i-public", "203.0.113.10"),
		{ID: "igw-1", Type: aws.TypeInternetGateway, Name: "igw", Parent: "vpc-1"},
	}}}}
}

func TestSubnetGrid(t *testing.T) {
	out, err := Render(context.Background(), "grid", gridModel(), layout.ELK{}, config.Colors{})
	if err != nil {
		t.Fatal(err)
	}
	var p parsed
	if err := xml.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	type geo struct{ x, y, w, h float64 }
	at, parent := map[string]geo{}, map[string]string{}
	for _, o := range p.Objects {
		at[o.ID] = geo{o.Cell.Geo.X, o.Cell.Geo.Y, o.Cell.Geo.Width, o.Cell.Geo.Height}
		parent[o.ID] = o.Cell.Parent
	}

	// Subnets are drawn in the VPC; the grid block and spacers are not drawn.
	for _, id := range []string{"pub-a", "priv-a", "priv-a2", "priv-b"} {
		if parent[id] != "vpc-1" {
			t.Errorf("%s drawn in %q, want vpc-1", id, parent[id])
		}
	}
	for id := range at {
		if id == "vpc-1-subnets" || id == "vpc-1-resources" || len(id) > 14 && id[:14] == "vpc-1-subnets-" {
			t.Errorf("layout-only item %s was drawn", id)
		}
	}
	// Zone a is left of zone b; in zone a, public is above private, and the
	// two private subnets stack by name.
	if !(at["pub-a"].x < at["priv-b"].x) {
		t.Errorf("zone a %+v is not left of zone b %+v", at["pub-a"], at["priv-b"])
	}
	if !(at["pub-a"].y < at["priv-a"].y && at["priv-a"].y < at["priv-a2"].y) {
		t.Errorf(
			"zone a order: pub %+v, priv %+v, priv2 %+v; want public on top",
			at["pub-a"],
			at["priv-a"],
			at["priv-a2"],
		)
	}
	// priv-b is in the first private row, beside priv-a, not under a gap.
	if at["priv-b"].y != at["priv-a"].y {
		t.Errorf("priv-b y %v, want the first private row %v", at["priv-b"].y, at["priv-a"].y)
	}
	// The VPC's loose gateway is right of every subnet.
	for _, id := range []string{"pub-a", "priv-a", "priv-b"} {
		if at["igw-1"].x < at[id].x+at[id].w {
			t.Errorf("igw-1 %+v is not right of %s %+v", at["igw-1"], id, at[id])
		}
	}
	// In priv-a, the public instance comes first.
	pub, priv := at["i-public"], at["i-private"]
	if !(pub.y < priv.y || pub.y == priv.y && pub.x < priv.x) {
		t.Errorf("i-public %+v does not come before i-private %+v", at["i-public"], at["i-private"])
	}
}

func TestPublicFirst(t *testing.T) {
	if publicFirst(true, false) != -1 || publicFirst(false, true) != 1 || publicFirst(true, true) != 0 {
		t.Error("publicFirst does not put public first")
	}
}
