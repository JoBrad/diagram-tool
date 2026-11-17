package drawio

import (
	"context"
	"encoding/xml"
	"reflect"
	"strings"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/layout"
	"diagram/internal/model"
	"diagram/internal/platform/aws"
)

func testModel() *model.Diagram {
	port := func(p int32) *int32 { return &p }
	inst := func(id, name, subnet, instanceType, ip string) *model.Node {
		return &model.Node{ID: id, Type: aws.TypeInstance, Name: name, Parent: subnet,
			Attrs: map[string]string{"instance_type": instanceType, "private_ip": ip}}
	}
	return &model.Diagram{Regions: []*model.Region{{
		Name: "us-east-1",
		Nodes: []*model.Node{
			inst("i-web", "web", "subnet-pub", "t3.small", "10.0.0.10"),
			inst("i-db", "db", "subnet-priv", "r5.large", "10.0.1.10"),
			inst("i-new", "new", "subnet-priv", "m6i.large", "10.0.1.11"),
			inst("i-old", "old", "subnet-priv", "m3.medium", "10.0.1.12"),
			{ID: "vgw-1", Type: aws.TypeVPNGateway, Name: "vgw", Parent: "vpc-1"},
			{ID: "arn:db-1", Type: "aws_example_db", Name: "orders", Parent: "vpc-1"},
			{ID: "arn:lb-1", Type: "AWS::ElasticLoadBalancingV2::LoadBalancer", Name: "web-lb", Parent: "vpc-1"},
			{ID: "cgw-1", Type: aws.TypeCustomerGateway, Name: "office", Attrs: map[string]string{"ip": "203.0.113.1"}},
			{ID: "pl-1", Type: aws.TypePrefixList, Name: "partners"},
			{ID: "vpc-1", Type: aws.TypeVPC, Name: "main", Attrs: map[string]string{"cidr": "10.0.0.0/16"}},
			{
				ID: "subnet-pub", Type: aws.TypeSubnet, Name: "public", Parent: "vpc-1", Tier: inventory.TierPublic,
				Attrs: map[string]string{"cidr": "10.0.0.0/24"},
			},
			{
				ID: "subnet-priv", Type: aws.TypeSubnet, Name: "private", Parent: "vpc-1", Tier: inventory.TierPrivate,
				Attrs: map[string]string{"cidr": "10.0.1.0/24"},
			},
			{ID: "sg-web", Type: aws.TypeSecurityGroup, Name: "web", Parent: "vpc-1", Rules: []model.Rule{
				{
					Protocol:   "tcp",
					FromPort:   port(443),
					ToPort:     port(443),
					TargetType: aws.TargetPrefixList,
					Target:     "pl-1",
				},
			}},
			{ID: "sg-db", Type: aws.TypeSecurityGroup, Name: "db", Parent: "vpc-1", Rules: []model.Rule{
				{
					Protocol:   "tcp",
					FromPort:   port(5432),
					ToPort:     port(5432),
					TargetType: inventory.TargetGroup,
					Target:     "sg-web",
				},
			}},
		},
		Edges: []*model.Edge{
			{Relation: model.RelMemberOf, From: "i-web", To: "sg-web"},
			{Relation: model.RelMemberOf, From: "i-db", To: "sg-db"},
			{ID: "vpn-1", Relation: aws.RelVPNConnection, From: "vgw-1", To: "cgw-1"},
		},
	}}}
}

// parsed is the subset of draw.io XML the test reads back.
type parsed struct {
	Objects []struct {
		ID    string `xml:"id,attr"`
		Label string `xml:"label,attr"`
		Cell  struct {
			Parent string `xml:"parent,attr"`
			Style  string `xml:"style,attr"`
			Vertex string `xml:"vertex,attr"`
			Edge   string `xml:"edge,attr"`
			Source string `xml:"source,attr"`
			Target string `xml:"target,attr"`
			Geo    struct {
				X      float64 `xml:"x,attr"`
				Y      float64 `xml:"y,attr"`
				Width  float64 `xml:"width,attr"`
				Height float64 `xml:"height,attr"`
			} `xml:"mxGeometry"`
		} `xml:"mxCell"`
	} `xml:"diagram>mxGraphModel>root>UserObject"`
}

func TestRender(t *testing.T) {
	out, err := Render(context.Background(), "test", testModel(), layout.ELK{}, config.Colors{})
	if err != nil {
		t.Fatal(err)
	}
	var p parsed
	if err := xml.Unmarshal(out, &p); err != nil {
		t.Fatalf("output is not valid XML: %v", err)
	}

	type geo struct{ x, y, w, h float64 }
	vertices := map[string]geo{}
	parents := map[string]string{}
	for _, o := range p.Objects {
		if o.Cell.Vertex == "1" {
			vertices[o.ID] = geo{o.Cell.Geo.X, o.Cell.Geo.Y, o.Cell.Geo.Width, o.Cell.Geo.Height}
			parents[o.ID] = o.Cell.Parent
		}
	}
	for _, want := range []string{
		"us-east-1", "vpc-1", "subnet-pub", "subnet-priv", "i-web", "i-db", "vgw-1", "cgw-1", "pl-1",
	} {
		if _, ok := vertices[want]; !ok {
			t.Errorf("no vertex for %s", want)
		}
	}
	wantParent := map[string]string{
		"vpc-1":      "us-east-1",
		"subnet-pub": "vpc-1",
		"i-web":      "sg-web-in-subnet-pub",
		"i-db":       "sg-db-in-subnet-priv",
		// Security groups are boxes inside the subnet that holds their members.
		"sg-web-in-subnet-pub": "subnet-pub",
		"sg-db-in-subnet-priv": "subnet-priv",
		"vgw-1":                "vpc-1",
		"cgw-1":                "us-east-1",
		"arn:db-1":             "vpc-1",
	}
	for id, want := range wantParent {
		if parents[id] != want {
			t.Errorf("%s: parent %q, want %q", id, parents[id], want)
		}
	}
	// Geometry is relative to the parent, so a child must fit in its
	// parent's width and height from the origin.
	for id, parent := range parents {
		c, pg := vertices[id], vertices[parent]
		if parent == "1" {
			continue
		}
		if c.x < 0 || c.y < 0 || c.x+c.w > pg.w || c.y+c.h > pg.h {
			t.Errorf("%s %+v does not fit in parent %s %+v", id, c, parent, pg)
		}
	}

	// What sits in the region itself is laid out to the right of the VPCs
	// and drawn directly in the region; the block holding it is not drawn.
	vpc := vertices["vpc-1"]
	for _, id := range []string{"cgw-1", "pl-1"} {
		if parents[id] != "us-east-1" || vertices[id].x < vpc.x+vpc.w {
			t.Errorf("%s at %+v in %q, want it in the region, right of vpc-1 %+v", id, vertices[id], parents[id], vpc)
		}
	}
	if _, ok := vertices["us-east-1-regional"]; ok {
		t.Error("the layout-only block was drawn")
	}

	// A generic resource fills its slot instead of shrinking to an icon.
	if g := vertices["arn:db-1"]; g.w != slotWidth || g.h != slotHeight {
		t.Errorf("generic box %+v, want %vx%v", g, slotWidth, slotHeight)
	}

	shapes := map[string]string{}
	styles := map[string]string{}
	for _, o := range p.Objects {
		styles[o.ID] = o.Cell.Style
		for _, part := range strings.Split(o.Cell.Style, ";") {
			if shape, ok := strings.CutPrefix(part, "shape="); ok {
				shapes[o.ID] = shape
			}
		}
	}
	for id, want := range map[string]string{
		"i-web": "mxgraph.aws4.t3_instance",
		"i-db":  "mxgraph.aws4.r5_instance",
		"i-new": "mxgraph.aws4.ec2_m6i_instance",
		"i-old": "mxgraph.aws4.instance2", // no m3 icon in draw.io
	} {
		if shapes[id] != want {
			t.Errorf("%s: shape %q, want %q", id, shapes[id], want)
		}
	}
	if shapes["arn:db-1"] != "" || !strings.HasPrefix(styles["arn:db-1"], genericStyle) {
		t.Errorf("generic style = %q, want the plain box with no shape", styles["arn:db-1"])
	}
	// A generic resource type with a palette entry gets its resource icon.
	lb := styles["arn:lb-1"]
	if shapes["arn:lb-1"] != "mxgraph.aws4.resourceIcon" ||
		!strings.Contains(
			lb,
			"resIcon=mxgraph.aws4.elastic_load_balancing;",
		) || !strings.Contains(lb, "fillColor=#8C4FFF;") {
		t.Errorf("load balancer style = %q, want the elastic_load_balancing resource icon", lb)
	}
	if g := vertices["arn:lb-1"]; g.w != iconSize || g.h != iconSize {
		t.Errorf("load balancer box %+v, want an icon %vx%v", g, iconSize, iconSize)
	}

	edges := map[string][2]string{}
	labels := map[string]string{}
	for _, o := range p.Objects {
		if o.Cell.Edge == "1" {
			edges[o.ID] = [2]string{o.Cell.Source, o.Cell.Target}
			labels[o.ID] = o.Label
			for _, end := range []string{o.Cell.Source, o.Cell.Target} {
				if _, ok := vertices[end]; !ok {
					t.Errorf("edge %s: end %s is not a vertex", o.ID, end)
				}
			}
		}
	}
	for _, o := range p.Objects {
		// Labeled edges carry their label near the source end.
		if o.Cell.Edge == "1" && o.Label != "" && o.Cell.Geo.X != edgeLabelAt {
			t.Errorf("edge %s label at x=%v, want %v", o.ID, o.Cell.Geo.X, edgeLabelAt)
		}
		if o.Cell.Edge == "1" && !strings.Contains(o.Cell.Style, "edgeStyle=orthogonalEdgeStyle;libavoidRouting=1;") {
			t.Errorf("edge %s style %q, want orthogonal, obstacle-avoiding routing", o.ID, o.Cell.Style)
		}
	}
	for id, want := range map[string]struct {
		ends  [2]string
		label string
	}{
		"vpn-1": {[2]string{"vgw-1", "cgw-1"}, ""},
		// Rule edges join the security group boxes, not their members.
		"rule-pl-1-sg-web-in-subnet-pub": {[2]string{"pl-1", "sg-web-in-subnet-pub"}, "tcp/443"},
		"rule-sg-web-in-subnet-pub-sg-db-in-subnet-priv": {
			[2]string{"sg-web-in-subnet-pub", "sg-db-in-subnet-priv"}, "tcp/5432",
		},
	} {
		if edges[id] != want.ends || labels[id] != want.label {
			t.Errorf("edge %s: %v %q, want %v %q", id, edges[id], labels[id], want.ends, want.label)
		}
	}
}

func TestIconShape(t *testing.T) {
	for instanceType, want := range map[string]string{
		"t3.small":           "t3_instance",
		"m6i.large":          "ec2_m6i_instance",
		"c5a.xlarge":         "c5a",
		"inf1.xlarge":        "inf1",
		"mac1.metal":         "mac_instance",
		"mac2.metal":         "ec2_m1_mac_instance",
		"mac2-m2.metal":      "mac_instance",
		"u-6tb1.56xlarge":    "high_memory_instance",
		"u7i-12tb.224xlarge": "high_memory_instance",
		"trn2.48xlarge":      "trainium_instance",
		"c7i.large":          "optimized_instance",
		"c6i.large":          "ec2_c6i_instance",
		"m7i.large":          "instance2",
	} {
		n := &model.Node{Type: aws.TypeInstance, Attrs: map[string]string{"instance_type": instanceType}}
		if got := iconShape(n); got != "mxgraph.aws4."+want {
			t.Errorf("%s: %s, want mxgraph.aws4.%s", instanceType, got, want)
		}
	}
}

func TestWaypoints(t *testing.T) {
	pt := func(x, y float64) layout.Point { return layout.Point{X: x, Y: y} }
	if got := waypoints(nil); got != nil {
		t.Errorf("waypoints(nil) = %+v, want nil", got)
	}
	if got := waypoints([]layout.Point{pt(0, 0), pt(100, 0)}); got != nil {
		t.Errorf("straight route = %+v, want no waypoints", got)
	}
	// Only the bends between the ends become waypoints.
	got := waypoints([]layout.Point{pt(0, 0), pt(0, 50), pt(100, 50), pt(100, 80)})
	want := &mxArray{As: "points", Points: []mxPoint{{X: 0, Y: 50}, {X: 100, Y: 50}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("waypoints = %+v, want %+v", got, want)
	}
}
