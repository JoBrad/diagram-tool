package aws

import (
	"slices"
	"testing"

	"diagram/internal/config"
	"diagram/internal/inventory"
	"diagram/internal/model"
)

// These cases run Finish and model.Build together, so they check what the
// AWS selection rules mean once the model prunes by them.

// placed returns a record of type rt in vpc and subnet. Either may be empty.
func placed(id, rt, vpc, subnet string) inventory.Record {
	r := rec(id, rt, id)
	r.Refs = placement(vpc, subnet)
	return r
}

// sgWith returns a security group in vpc with an inbound rule per prefix list.
func sgWith(id, vpc string, prefixLists ...string) inventory.Record {
	r := placed(id, TypeSecurityGroup, vpc, "")
	for _, pl := range prefixLists {
		r.Rules = append(r.Rules, inventory.Rule{Protocol: "-1", TargetType: TargetPrefixList, Target: pl})
	}
	return r
}

// joined returns r as a member of groups.
func joined(r inventory.Record, groups ...string) inventory.Record {
	for _, g := range groups {
		r.Refs = append(r.Refs, memberOf(g))
	}
	return r
}

// vpnRec returns a VPN connection from a gateway to a customer gateway.
func vpnRec(id, gateway, cgw string) inventory.Record {
	r := rec(id, TypeVPNConnection, "")
	r.From, r.To, r.Relation = gateway, cgw, RelVPNConnection
	return r
}

func cfgOf(global *bool, ec2 queries) *config.Config {
	return &config.Config{
		Cloud:   config.Clouds{AWS: &config.AWSAccount{Resources: *ec2Resources(ec2)}},
		Options: config.Options{ShowUnconnectedResources: global},
	}
}

func TestSelection(t *testing.T) {
	no := false
	yes := true
	tests := []struct {
		name    string
		cfg     *config.Config
		records []inventory.Record
		want    []string
	}{
		{
			name: "default keeps everything",
			cfg:  cfgOf(nil, nil),
			records: []inventory.Record{
				placed("vpc-empty", TypeVPC, "", ""), placed("cgw-spare", TypeCustomerGateway, "", ""),
			},
			want: []string{"vpc-empty", "cgw-spare"},
		},
		{
			// i-loose has no VPC, subnet, or group and is still kept:
			// instances are what the config asks to draw.
			name: "unconnected resources go, never instances",
			cfg:  cfgOf(&no, nil),
			records: []inventory.Record{
				joined(placed("i-1", TypeInstance, "vpc-a", "subnet-a"), "sg-a"),
				placed("i-loose", TypeInstance, "", ""),
				placed("vpc-a", TypeVPC, "", ""),
				placed("vpc-empty", TypeVPC, "", ""),
				placed("subnet-a", TypeSubnet, "vpc-a", ""),
				placed("subnet-empty", TypeSubnet, "vpc-a", ""),
				sgWith("sg-a", "vpc-a", "pl-used"),
				sgWith("sg-unused", "vpc-a"),
				placed("pl-used", TypePrefixList, "", ""),
				placed("pl-unused", TypePrefixList, "", ""),
				placed("cgw-spare", TypeCustomerGateway, "", ""),
			},
			want: []string{"i-1", "i-loose", "vpc-a", "subnet-a", "sg-a", "pl-used", "member_of:i-1->sg-a"},
		},
		{
			name: "dropping a VPN connection with a missing end cascades to its gateway",
			cfg:  cfgOf(&no, nil),
			records: []inventory.Record{
				placed("cgw-1", TypeCustomerGateway, "", ""),
				placed("cgw-2", TypeCustomerGateway, "", ""),
				placed("tgw-1", TypeTransitGateway, "", ""),
				placed("tgw-2", TypeTransitGateway, "", ""),
				vpnRec("vpn-orphan", "vgw-missing", "cgw-1"),
				vpnRec("vpn-ok", "tgw-1", "cgw-2"),
				vpnRec("vpn-half", "tgw-2", "cgw-missing"),
			},
			want: []string{"cgw-2", "tgw-1", "vpn-ok"},
		},
		{
			// A subnet kept by ID is content, so it keeps its VPC.
			name: "IDs on subnets keep an empty subnet and its VPC",
			cfg:  cfgOf(&no, queries{"subnets": {IDs: []string{"subnet-x"}}}),
			records: []inventory.Record{
				placed("vpc-x", TypeVPC, "", ""), placed("subnet-x", TypeSubnet, "vpc-x", ""),
			},
			want: []string{"vpc-x", "subnet-x"},
		},
		{
			// Every default VPC has an internet gateway; the pair must not
			// keep each other.
			name: "an empty VPC and its internet gateway both go",
			cfg:  cfgOf(&no, nil),
			records: []inventory.Record{
				placed("vpc-d", TypeVPC, "", ""),
				placed("igw-d", TypeInternetGateway, "vpc-d", ""),
				placed("igw-detached", TypeInternetGateway, "", ""),
			},
		},
		{
			name: "an internet gateway is kept with a kept VPC",
			cfg:  cfgOf(&no, nil),
			records: []inventory.Record{
				placed("i-1", TypeInstance, "vpc-a", "subnet-a"),
				placed("vpc-a", TypeVPC, "", ""),
				placed("subnet-a", TypeSubnet, "vpc-a", ""),
				placed("igw-a", TypeInternetGateway, "vpc-a", ""),
			},
			want: []string{"i-1", "vpc-a", "subnet-a", "igw-a"},
		},
		{
			name: "an empty VPC whose VPN gateway a kept VPN connection uses is kept",
			cfg:  cfgOf(&no, nil),
			records: []inventory.Record{
				placed("vpc-v", TypeVPC, "", ""),
				placed("vgw-v", TypeVPNGateway, "vpc-v", ""),
				placed("cgw-v", TypeCustomerGateway, "", ""),
				vpnRec("vpn-v", "vgw-v", "cgw-v"),
			},
			want: []string{"vpc-v", "vgw-v", "cgw-v", "vpn-v"},
		},
		{
			name: "an empty VPC with an unused VPN gateway goes with it",
			cfg:  cfgOf(&no, nil),
			records: []inventory.Record{
				placed("vpc-v", TypeVPC, "", ""),
				placed("vgw-v", TypeVPNGateway, "vpc-v", ""),
				vpnRec("vpn-v", "vgw-v", "cgw-missing"),
			},
		},
		{
			// nat-a's VPC holds an instance in another subnet, which keeps
			// the NAT gateway and so its own subnet. nat-b's VPC holds no
			// instance, so it goes, then its subnet, then its VPC.
			name: "a NAT gateway is kept only while its VPC holds a kept instance",
			cfg:  cfgOf(&no, nil),
			records: []inventory.Record{
				placed("i-1", TypeInstance, "vpc-a", "subnet-a1"),
				placed("vpc-a", TypeVPC, "", ""),
				placed("subnet-a1", TypeSubnet, "vpc-a", ""),
				placed("subnet-a2", TypeSubnet, "vpc-a", ""),
				placed("nat-a", TypeNATGateway, "vpc-a", "subnet-a2"),
				placed("vpc-b", TypeVPC, "", ""),
				placed("subnet-b", TypeSubnet, "vpc-b", ""),
				placed("nat-b", TypeNATGateway, "vpc-b", "subnet-b"),
			},
			want: []string{"i-1", "vpc-a", "subnet-a1", "subnet-a2", "nat-a"},
		},
		{
			// The security group is shown by override, so the prefix list it
			// references stays while prefix lists are pruned.
			name: "a prefix list is kept by a security group shown by override",
			cfg: cfgOf(&no, queries{
				"security_groups": {ShowUnconnectedResources: &yes},
			}),
			records: []inventory.Record{
				sgWith("sg-empty", "", "pl-1"),
				placed("pl-1", TypePrefixList, "", ""),
				placed("pl-2", TypePrefixList, "", ""),
			},
			want: []string{"sg-empty", "pl-1"},
		},
		{
			name: "a generic resource keeps its subnet, VPC, and security group",
			cfg:  cfgOf(&no, nil),
			records: []inventory.Record{
				placed("vpc-1", TypeVPC, "", ""),
				placed("vpc-2", TypeVPC, "", ""),
				placed("subnet-a", TypeSubnet, "vpc-1", ""),
				sgWith("sg-1", "vpc-1"),
				sgWith("sg-2", "vpc-1"),
				Generic("aws_example", "arn:eni-1", "eni", "vpc-1", []string{"subnet-a"}, []string{"sg-1"}),
			},
			want: []string{"vpc-1", "subnet-a", "sg-1", "arn:eni-1", "member_of:arn:eni-1->sg-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			regions := []*inventory.Region{{Name: "us-east-1", Records: tt.records}}
			Finish(tt.cfg, regions)
			r := model.Build(regions).Regions[0]
			var got []string
			for _, n := range r.Nodes {
				got = append(got, n.ID)
			}
			for _, e := range r.Edges {
				if e.ID != "" {
					got = append(got, e.ID)
				} else {
					got = append(got, string(e.Relation)+":"+e.From+"->"+e.To)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("kept %v, want %v", got, tt.want)
			}
		})
	}
}
