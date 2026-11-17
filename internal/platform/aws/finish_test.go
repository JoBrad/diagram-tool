package aws

import (
	"slices"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
)

func subnetRec(id, vpc, zone string) inventory.Record {
	r := rec(id, TypeSubnet, id)
	r.Zone = zone
	r.Refs = []inventory.Ref{in(TypeVPC, vpc)}
	return r
}

func TestResolveParents(t *testing.T) {
	inSubnets := func(id string, subnets ...string) inventory.Record {
		return Generic("AWS::Lambda::Function", id, id, "vpc-1", subnets, []string{"sg-1"})
	}
	region := &inventory.Region{Name: "us-east-1", Records: []inventory.Record{
		rec("vpc-1", TypeVPC, "vpc-1"),
		subnetRec("subnet-1", "vpc-1", "us-east-1c"),
		subnetRec("subnet-2", "vpc-1", "us-east-1a"),
		subnetRec("subnet-3", "vpc-1", "us-east-1a"),
		inSubnets("zone-wins", "subnet-1", "subnet-3", "subnet-2"),
		inSubnets("unknown-last", "subnet-9", "subnet-1"),
		// Neither subnet is in the region, so it falls back to the VPC.
		inSubnets("unknowns-fall-back", "subnet-9", "subnet-8"),
		inSubnets("single", "subnet-1"),
		Generic("AWS::Lambda::Function", "no-vpc", "no-vpc", "", nil, nil),
		Generic("AWS::Lambda::Function", "missing-vpc", "missing-vpc", "vpc-9", nil, nil),
	}}
	vpcOf := resolveParents(region)

	want := map[string]string{
		"vpc-1":              "",
		"subnet-1":           "vpc-1",
		"zone-wins":          "subnet-2", // us-east-1a, and the lower ID of the two there
		"unknown-last":       "subnet-1",
		"unknowns-fall-back": "vpc-1",
		"single":             "subnet-1",
		"no-vpc":             "",
		"missing-vpc":        "",
	}
	for _, r := range region.Records {
		if w, ok := want[r.ID]; ok && r.Parent != w {
			t.Errorf("%s parent = %q, want %q", r.ID, r.Parent, w)
		}
		for _, ref := range r.Refs {
			if ref.Relation == inventory.RelContainedIn {
				t.Errorf("%s kept containment ref %+v", r.ID, ref)
			}
		}
	}
	if got := region.Records[4].Refs; len(got) != 1 || got[0] != memberOf("sg-1") {
		t.Errorf("zone-wins refs = %+v, want only its security group", got)
	}
	if vpcOf["missing-vpc"] != "vpc-9" || vpcOf["subnet-1"] != "vpc-1" {
		t.Errorf("vpcOf = %v, want the VPCs as referenced", vpcOf)
	}
}

func TestConnect(t *testing.T) {
	withVPC := func(id, rt, vpc string) inventory.Record {
		r := rec(id, rt, id)
		r.Refs = []inventory.Ref{in(TypeVPC, vpc)}
		return r
	}
	inst := withVPC("i-1", TypeInstance, "vpc-1")
	inst.Refs = append(inst.Refs, memberOf("sg-1"))
	sg := withVPC("sg-1", TypeSecurityGroup, "vpc-1")
	sg.Rules = []inventory.Rule{
		{TargetType: TargetPrefixList, Target: "pl-1"},
		{Egress: true, TargetType: TargetPrefixList, Target: "pl-2"},
	}
	vpn := func(id, from, to string) inventory.Record {
		r := rec(id, TypeVPNConnection, "")
		r.From, r.To, r.Relation = from, to, RelVPNConnection
		return r
	}
	region := &inventory.Region{Records: []inventory.Record{
		inst, sg,
		rec("vpc-1", TypeVPC, "vpc-1"),
		withVPC("igw-1", TypeInternetGateway, "vpc-1"),
		rec("igw-loose", TypeInternetGateway, "igw-loose"),
		withVPC("vgw-1", TypeVPNGateway, "vpc-1"),
		withVPC("nat-1", TypeNATGateway, "vpc-1"),
		rec("tgw-1", TypeTransitGateway, "tgw-1"),
		rec("cgw-1", TypeCustomerGateway, "cgw-1"),
		rec("pl-1", TypePrefixList, "pl-1"),
		rec("pl-2", TypePrefixList, "pl-2"),
		vpn("vpn-1", "vgw-1", "cgw-1"),
		vpn("vpn-2", "tgw-1", "cgw-1"),
		Generic("AWS::Lambda::Function", "fn", "fn", "", nil, nil),
	}}
	connect(region, resolveParents(region))

	type keep struct {
		keepIf   []string
		attached bool
	}
	want := map[string]keep{
		"sg-1":      {keepIf: []string{"i-1"}},
		"vpc-1":     {keepIf: []string{"vpn-1"}},
		"igw-1":     {keepIf: []string{"vpc-1"}, attached: true},
		"igw-loose": {attached: true},
		"vgw-1":     {keepIf: []string{"vpc-1", "vpn-1"}, attached: true},
		"nat-1":     {keepIf: []string{"i-1"}},
		"tgw-1":     {keepIf: []string{"vpn-2"}},
		"cgw-1":     {keepIf: []string{"vpn-1", "vpn-2"}},
		"pl-1":      {keepIf: []string{"sg-1"}},
		"pl-2":      {keepIf: []string{"sg-1"}},
	}
	for _, r := range region.Records {
		w := want[r.ID]
		if !slices.Equal(r.KeepIf, w.keepIf) || r.Attached != w.attached {
			t.Errorf("%s keepIf = %v, attached = %v; want %v, %v", r.ID, r.KeepIf, r.Attached, w.keepIf, w.attached)
		}
	}
}

func TestApplyConfig(t *testing.T) {
	boolp := func(b bool) *bool { return &b }
	records := func() []inventory.Record {
		return []inventory.Record{
			rec("i-1", TypeInstance, ""), rec("vpc-1", TypeVPC, ""), rec("subnet-1", TypeSubnet, ""),
			rec("cgw-1", TypeCustomerGateway, ""), rec("vpn-1", TypeVPNConnection, ""), rec("key-1", TypeKMSKey, ""),
		}
	}
	prunable := func(recs []inventory.Record) []string {
		var out []string
		for _, r := range recs {
			if r.Prunable {
				out = append(out, r.ID)
			}
		}
		return out
	}
	tests := []struct {
		name   string
		global *bool
		ec2    *config.EC2Resources
		want   []string
	}{
		{"default prunes nothing", nil, nil, nil},
		{"global true prunes nothing", boolp(true), &config.EC2Resources{}, nil},
		{"global false prunes every prunable type", boolp(false), nil, []string{"vpc-1", "subnet-1", "cgw-1", "vpn-1"}},
		{
			"per-type true overrides global false", boolp(false),
			&config.EC2Resources{Subnets: &config.ResourceQuery{ShowUnconnectedResources: boolp(true)}},
			[]string{"vpc-1", "cgw-1", "vpn-1"},
		},
		{
			"per-type false with global unset", nil,
			&config.EC2Resources{VPCs: &config.ResourceQuery{ShowUnconnectedResources: boolp(false)}},
			[]string{"vpc-1"},
		},
		{
			"IDs exempt a type", boolp(false),
			&config.EC2Resources{CustomerGateways: &config.ResourceQuery{IDs: []string{"cgw-1"}}},
			[]string{"vpc-1", "subnet-1", "vpn-1"},
		},
		{
			"instances are never pruned", boolp(false),
			&config.EC2Resources{Instances: &config.ResourceQuery{ShowUnconnectedResources: boolp(false)}},
			[]string{"vpc-1", "subnet-1", "cgw-1", "vpn-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recs := records()
			applyConfig(recs, &config.Resources{EC2: tt.ec2}, tt.global)
			if got := prunable(recs); !slices.Equal(got, tt.want) {
				t.Errorf("prunable = %v, want %v", got, tt.want)
			}
		})
	}

	recs := records()
	applyConfig(recs, &config.Resources{EC2: &config.EC2Resources{
		Instances: &config.ResourceQuery{Hide: true},
		VPCs:      &config.ResourceQuery{HideChildren: true},
	}}, nil)
	if !recs[0].Hidden || recs[0].HideChildren || recs[1].Hidden || !recs[1].HideChildren || recs[2].Hidden {
		t.Errorf("hide flags = %+v", recs[:3])
	}
}

func TestFinish(t *testing.T) {
	f := false
	cfg := &config.Config{
		Cloud:   config.Clouds{AWSGov: &config.AWSAccount{}},
		Options: config.Options{ShowUnconnectedResources: &f},
	}
	sub := subnetRec("subnet-1", "vpc-1", "us-gov-west-1a")
	regions := []*inventory.Region{{Name: "us-gov-west-1", Records: []inventory.Record{
		rec("vpc-1", TypeVPC, "vpc-1"), sub,
	}}}
	Finish(cfg, regions)
	got := regions[0].Records[1]
	if got.Parent != "vpc-1" || !got.Prunable || got.Attached || len(got.Refs) != 0 {
		t.Errorf("subnet = %+v, want parent vpc-1, prunable, not attached, no refs", got)
	}
}
