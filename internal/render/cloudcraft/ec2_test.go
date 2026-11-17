package cloudcraft

import (
	"reflect"
	"slices"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/model"
	"diagram/internal/platform/aws"
)

// ec2Model is one VPC with a public subnet holding a web instance and a NAT
// gateway, a private subnet holding a database instance, an empty subnet,
// security groups with and without members, gateways, a prefix list, and VPN
// connections, one of which ends at a gateway that is not drawn.
func ec2Model() *model.Diagram {
	port := func(p int32) *int32 { return &p }
	inst := func(id, name, subnet, instanceType, platform, ip string) *model.Node {
		return &model.Node{ID: id, Type: aws.TypeInstance, Name: name, Parent: subnet, Attrs: map[string]string{
			"instance_type": instanceType, "platform_details": platform, "private_ip": ip,
		}}
	}
	subnet := func(id, name, cidr, zone string, tier inventory.Tier) *model.Node {
		return &model.Node{
			ID: id, Type: aws.TypeSubnet, Name: name, Parent: "vpc-1", Zone: zone, Tier: tier,
			Attrs: map[string]string{"cidr": cidr},
		}
	}
	return &model.Diagram{Regions: []*model.Region{{
		Name: "us-east-1",
		Nodes: []*model.Node{
			inst("i-web", "web", "subnet-pub", "t3.small", "Linux/UNIX", "10.0.0.10"),
			inst("i-db", "db", "subnet-priv", "r5.large", "Windows", "10.0.1.10"),
			{
				ID: "nat-1", Type: aws.TypeNATGateway, Name: "nat", Parent: "subnet-pub",
				Attrs: map[string]string{"public_ips": "203.0.113.20,203.0.113.21"},
			},
			{ID: "vgw-1", Type: aws.TypeVPNGateway, Name: "vgw", Parent: "vpc-1"},
			{ID: "igw-1", Type: aws.TypeInternetGateway, Name: "igw", Parent: "vpc-1"},
			{ID: "tgw-1", Type: aws.TypeTransitGateway, Name: "hub"},
			{
				ID:    "cgw-1",
				Type:  aws.TypeCustomerGateway,
				Name:  "office",
				Attrs: map[string]string{"ip": "198.51.100.1"},
			},
			{ID: "pl-1", Type: aws.TypePrefixList, Name: "partners"},
			{ID: "arn:fn", Type: "AWS::Lambda::Function", Name: "fn", Parent: "subnet-priv"},
			{ID: "vpc-1", Type: aws.TypeVPC, Name: "main", Attrs: map[string]string{"cidr": "10.0.0.0/16"}},
			subnet("subnet-pub", "public", "10.0.0.0/24", "us-east-1a", inventory.TierPublic),
			subnet("subnet-priv", "private", "10.0.1.0/24", "us-east-1a", inventory.TierPrivate),
			subnet("subnet-empty", "spare", "10.0.2.0/24", "us-east-1b", inventory.TierPrivate),
			{ID: "sg-web", Type: aws.TypeSecurityGroup, Name: "web", Parent: "vpc-1", Rules: []model.Rule{
				{
					Protocol:   "tcp",
					FromPort:   port(443),
					ToPort:     port(443),
					TargetType: aws.TargetPrefixList,
					Target:     "pl-1",
				},
				{
					Protocol:   "tcp",
					FromPort:   port(80),
					ToPort:     port(80),
					TargetType: aws.TargetPrefixList,
					Target:     "pl-gone",
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
				{
					Protocol:   "tcp",
					FromPort:   port(22),
					ToPort:     port(22),
					TargetType: inventory.TargetGroup,
					Target:     "sg-gone",
				},
				{Egress: true, Protocol: "-1", TargetType: inventory.TargetCIDR, Target: "0.0.0.0/0"},
				{
					Egress:     true,
					Protocol:   "tcp",
					FromPort:   port(1024),
					ToPort:     port(65535),
					TargetType: inventory.TargetCIDRv6,
					Target:     "::/0",
				},
			}},
			{ID: "sg-empty", Type: aws.TypeSecurityGroup, Name: "empty", Parent: "vpc-1"},
		},
		Edges: []*model.Edge{
			{Relation: model.RelMemberOf, From: "i-web", To: "sg-web"},
			{Relation: model.RelMemberOf, From: "i-db", To: "sg-db"},
			{ID: "vpn-1", Relation: aws.RelVPNConnection, From: "vgw-1", To: "cgw-1"},
			{ID: "vpn-2", Relation: aws.RelVPNConnection, From: "tgw-1", To: "cgw-1"},
			{ID: "vpn-gone", Relation: aws.RelVPNConnection, From: "vgw-gone", To: "cgw-1"},
		},
	}}}
}

// components indexes every component of a blueprint by ID.
func components(t *testing.T, cfg *config.Config, m *model.Diagram) map[string]component {
	t.Helper()
	data := Build("test", cfg, m)
	out := map[string]component{}
	for _, list := range [][]component{data.Nodes, data.Groups, data.Edges, data.Text} {
		for _, c := range list {
			out[c["id"].(string)] = c
		}
	}
	return out
}

func TestBuildEC2Nodes(t *testing.T) {
	got := components(t, &config.Config{}, ec2Model())

	want := map[string]map[string]any{
		"i-web":                    {"type": "ec2", "platform": "linux", "instanceType": "t3", "instanceSize": "small"},
		"i-db":                     {"type": "ec2", "platform": "mswin", "instanceType": "r5", "instanceSize": "large"},
		"nat-1":                    {"type": "natgateway"},
		"vgw-1":                    {"type": "vpngateway"},
		"igw-1":                    {"type": "internetgateway"},
		"tgw-1":                    {"type": "transitgateway", "connections": 1},
		"cgw-1":                    {"type": "customergateway"},
		"subnet-empty-placeholder": {"type": "placeholder"},
		"sg-empty-placeholder":     {"type": "placeholder"},
		"pl-1-placeholder":         {"type": "placeholder"},
		"i-web-label":              {"text": "web\n10.0.0.10"},
		"nat-1-label":              {"text": "nat\n203.0.113.20\n203.0.113.21"},
		"cgw-1-label":              {"text": "office\n198.51.100.1"},
	}
	for id, fields := range want {
		c, ok := got[id]
		if !ok {
			t.Errorf("%s not drawn", id)
			continue
		}
		for k, v := range fields {
			if !reflect.DeepEqual(c[k], v) {
				t.Errorf("%s %s = %v, want %v", id, k, c[k], v)
			}
		}
	}
	// A generic resource has no Cloudcraft component; a VPN connection with
	// an end that is not drawn is skipped.
	for _, id := range []string{"arn:fn", "vpn-gone"} {
		if _, ok := got[id]; ok {
			t.Errorf("%s was drawn", id)
		}
	}
}

func TestBuildEC2Groups(t *testing.T) {
	got := components(t, &config.Config{}, ec2Model())

	nodes := func(id string) []string { return got[id]["nodes"].([]string) }
	groups := map[string][]string{
		"subnet-pub":   {"i-web", "nat-1"},
		"subnet-priv":  {"i-db"},
		"subnet-empty": {"subnet-empty-placeholder"},
		"sg-web":       {"i-web"},
		"sg-db":        {"i-db"},
		"sg-empty":     {"sg-empty-placeholder"},
		"pl-1":         {"pl-1-placeholder"},
	}
	for id, want := range groups {
		if !slices.Equal(nodes(id), want) {
			t.Errorf("%s nodes = %v, want %v", id, nodes(id), want)
		}
	}
	vpc := nodes("vpc-1")
	for _, id := range []string{"vgw-1", "igw-1", "subnet-pub", "subnet-priv", "subnet-empty", "sg-web", "sg-db"} {
		if !slices.Contains(vpc, id) {
			t.Errorf("vpc-1 nodes = %v, missing %s", vpc, id)
		}
	}
	if slices.Contains(vpc, "tgw-1") || slices.Contains(vpc, "cgw-1") {
		t.Errorf("vpc-1 nodes = %v, want no regional gateways", vpc)
	}

	pub := got["subnet-pub"]
	if pub["public"] != true || pub["availabilityZone"] != "us-east-1a" || pub["name"] != "public (10.0.0.0/24)" {
		t.Errorf("subnet-pub = %v", pub)
	}
	if got["subnet-priv"]["public"] != false || got["vpc-1"]["name"] != "main (10.0.0.0/16)" {
		t.Errorf("subnet-priv = %v, vpc-1 = %v", got["subnet-priv"], got["vpc-1"])
	}
	if got["pl-1"]["name"] != "prefix list: partners (pl-1)" || got["sg-web"]["name"] != "web (sg-web)" {
		t.Errorf("group names: %v, %v", got["pl-1"]["name"], got["sg-web"]["name"])
	}
}

func TestBuildEC2Rules(t *testing.T) {
	got := components(t, &config.Config{}, ec2Model())
	rule := func(protocol, ports, target, targetType string) component {
		return component{
			"protocol": protocol, "portRange": ports, "target": target, "targetType": targetType,
			"description": "", "hidden": false,
		}
	}
	tests := []struct {
		group, direction string
		want             []component
	}{
		// Rules to a prefix list or group that is not drawn are skipped.
		{"sg-web", "inboundRules", []component{rule("tcp", "443", "pl-1", "sg")}},
		{"sg-web", "outboundRules", []component{}},
		{"sg-db", "inboundRules", []component{rule("tcp", "5432", "sg-web", "sg")}},
		{"sg-db", "outboundRules", []component{
			rule("-1", "0", "0.0.0.0/0", "ip"), rule("tcp", "1024-65535", "::/0", "ipv6"),
		}},
	}
	for _, tt := range tests {
		if g := got[tt.group][tt.direction]; !reflect.DeepEqual(g, tt.want) {
			t.Errorf("%s %s = %v, want %v", tt.group, tt.direction, g, tt.want)
		}
	}

	for _, id := range []string{"vpn-1", "vpn-2"} {
		if e := got[id]; e["type"] != "vpnconnection" || e["to"] != "cgw-1" {
			t.Errorf("%s = %v, want a VPN connection to cgw-1", id, e)
		}
	}
}

// TestBuildEC2NoVPC checks resources whose VPC is not drawn, as when the
// config hides VPCs: they are placed outside any VPC.
func TestBuildEC2NoVPC(t *testing.T) {
	m := &model.Diagram{Regions: []*model.Region{{Name: "us-east-1", Nodes: []*model.Node{
		{ID: "subnet-1", Type: aws.TypeSubnet, Name: "loose", Attrs: map[string]string{"cidr": "10.0.0.0/24"}},
		{ID: "i-1", Type: aws.TypeInstance, Name: "i-1", Parent: "subnet-1", Attrs: map[string]string{}},
		{ID: "igw-1", Type: aws.TypeInternetGateway, Name: "igw"},
	}}}}
	got := components(t, &config.Config{}, m)
	if !slices.Equal(got["subnet-1"]["nodes"].([]string), []string{"i-1"}) {
		t.Errorf("subnet-1 nodes = %v, want i-1", got["subnet-1"]["nodes"])
	}
	if _, ok := got["igw-1"]; !ok {
		t.Error("internet gateway outside a VPC not drawn")
	}
}

func TestBuildOptions(t *testing.T) {
	hide := false
	cfg := &config.Config{Diagram: config.DiagramConfig{
		Projection: config.ProjectionNameState2D, Theme: config.ThemeNameStateDark, ShowGrid: &hide,
	}}
	data := Build("test", cfg, &model.Diagram{})
	if data.Projection != "2d" || data.Theme == nil || data.Theme.Base != "dark" ||
		!slices.Equal(data.DisabledLayers, []string{"grid"}) {
		t.Errorf("blueprint options = %+v", data)
	}
}

func TestEC2Platform(t *testing.T) {
	for details, want := range map[string]string{
		"Windows with SQL Server":  "mswin",
		"Red Hat Enterprise Linux": "rhel",
		"SUSE Linux":               "sles",
		"Linux/UNIX":               "linux",
		"":                         "linux",
	} {
		if got := ec2Platform(details); got != want {
			t.Errorf("ec2Platform(%q) = %q, want %q", details, got, want)
		}
	}
}
