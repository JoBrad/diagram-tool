package aws

import (
	"reflect"
	"strconv"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"diagram/internal/inventory"
)

var s = awssdk.String

func in(rt, id string) inventory.Ref { return ref(inventory.RelContainedIn, rt, id) }

func memberOf(id string) inventory.Ref { return ref(inventory.RelMemberOf, TypeSecurityGroup, id) }

// rec is an AWS record for comparison.
func rec(id, rt, name string) inventory.Record {
	return inventory.Record{ID: id, Type: rt, Platform: Platform, Name: name}
}

func check(t *testing.T, name string, got, want inventory.Record) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s =\n%+v\nwant\n%+v", name, got, want)
	}
}

func TestInstance(t *testing.T) {
	got, ok := Instance(ec2types.Instance{
		InstanceId: s("i-1"), Tags: NameTags("web"), VpcId: s("vpc-1"), SubnetId: s("subnet-1"),
		InstanceType: "t3.micro", PlatformDetails: s("Linux/UNIX"),
		PrivateIpAddress: s("10.0.0.5"), PublicIpAddress: s("203.0.113.10"),
		SecurityGroups: []ec2types.GroupIdentifier{{GroupId: s("sg-1")}},
	})
	want := rec("i-1", TypeInstance, "web")
	want.Refs = []inventory.Ref{in(TypeSubnet, "subnet-1"), in(TypeVPC, "vpc-1"), memberOf("sg-1")}
	want.Tier = inventory.TierPublic
	want.Attrs = map[string]string{
		"instance_type": "t3.micro", "platform_details": "Linux/UNIX",
		"private_ip": "10.0.0.5", "public_ip": "203.0.113.10",
	}
	if !ok {
		t.Fatal("instance skipped")
	}
	check(t, "instance", got, want)

	// No public IP leaves the tier unknown; no Name tag names it by ID.
	got, _ = Instance(ec2types.Instance{InstanceId: s("i-2")})
	if got.Tier != "" || got.Name != "i-2" {
		t.Errorf("instance without public IP or name = %+v", got)
	}
	if _, ok := Instance(ec2types.Instance{
		InstanceId: s("i-gone"), State: &ec2types.InstanceState{Name: ec2types.InstanceStateNameTerminated},
	}); ok {
		t.Error("terminated instance was kept")
	}
}

func TestGateways(t *testing.T) {
	nat, ok := NATGateway(ec2types.NatGateway{
		NatGatewayId: s("nat-1"),
		VpcId:        s("vpc-1"),
		SubnetId:     s("subnet-1"),
		NatGatewayAddresses: []ec2types.NatGatewayAddress{
			{PublicIp: s("203.0.113.20")},
			{},
			{PublicIp: s("203.0.113.21")},
		},
	})
	want := rec("nat-1", TypeNATGateway, "nat-1")
	want.Refs = []inventory.Ref{in(TypeSubnet, "subnet-1"), in(TypeVPC, "vpc-1")}
	want.Attrs = map[string]string{"public_ips": "203.0.113.20,203.0.113.21"}
	if !ok {
		t.Fatal("NAT gateway skipped")
	}
	check(t, "NAT gateway", nat, want)

	vgw, _ := VPNGateway(ec2types.VpnGateway{VpnGatewayId: s("vgw-1"), VpcAttachments: []ec2types.VpcAttachment{
		{VpcId: s("vpc-old"), State: ec2types.AttachmentStatusDetached},
		{VpcId: s("vpc-1"), State: ec2types.AttachmentStatusAttached},
	}})
	want = rec("vgw-1", TypeVPNGateway, "vgw-1")
	want.Refs = []inventory.Ref{in(TypeVPC, "vpc-1")}
	check(t, "VPN gateway", vgw, want)

	igw := InternetGateway(ec2types.InternetGateway{
		InternetGatewayId: s("igw-1"),
		Attachments:       []ec2types.InternetGatewayAttachment{{VpcId: s("vpc-1"), State: "available"}},
	})
	want = rec("igw-1", TypeInternetGateway, "igw-1")
	want.Refs = []inventory.Ref{in(TypeVPC, "vpc-1")}
	check(t, "internet gateway", igw, want)

	tgw, _ := TransitGateway(ec2types.TransitGateway{TransitGatewayId: s("tgw-1"), Tags: NameTags("hub")})
	check(t, "transit gateway", tgw, rec("tgw-1", TypeTransitGateway, "hub"))

	cgw, _ := CustomerGateway(ec2types.CustomerGateway{CustomerGatewayId: s("cgw-1"), IpAddress: s("198.51.100.1")})
	want = rec("cgw-1", TypeCustomerGateway, "cgw-1")
	want.Attrs = map[string]string{"ip": "198.51.100.1"}
	check(t, "customer gateway", cgw, want)

	vpn, _ := VPNConnection(ec2types.VpnConnection{
		VpnConnectionId: s("vpn-1"), TransitGatewayId: s("tgw-1"), CustomerGatewayId: s("cgw-1"),
	})
	want = rec("vpn-1", TypeVPNConnection, "")
	want.From, want.To, want.Relation = "tgw-1", "cgw-1", RelVPNConnection
	check(t, "VPN connection", vpn, want)
}

func TestGone(t *testing.T) {
	gone := map[string]bool{}
	_, gone["nat"] = NATGateway(ec2types.NatGateway{State: ec2types.NatGatewayStateFailed})
	_, gone["vgw"] = VPNGateway(ec2types.VpnGateway{State: ec2types.VpnStateDeleted})
	_, gone["tgw"] = TransitGateway(ec2types.TransitGateway{State: ec2types.TransitGatewayStateDeleted})
	_, gone["cgw"] = CustomerGateway(ec2types.CustomerGateway{State: s("deleted")})
	_, gone["vpn"] = VPNConnection(ec2types.VpnConnection{State: ec2types.VpnStateDeleted})
	for kind, ok := range gone {
		if ok {
			t.Errorf("gone %s was kept", kind)
		}
	}
}

func TestNetworks(t *testing.T) {
	vpc := VPC(ec2types.Vpc{VpcId: s("vpc-1"), CidrBlock: s("10.0.0.0/16")})
	want := rec("vpc-1", TypeVPC, "vpc-1")
	want.Attrs = map[string]string{"cidr": "10.0.0.0/16"}
	check(t, "VPC", vpc, want)

	for _, public := range []bool{true, false} {
		subnet := Subnet(ec2types.Subnet{
			SubnetId: s("subnet-1"), VpcId: s("vpc-1"), CidrBlock: s("10.0.0.0/24"),
			AvailabilityZone: s("us-east-1a"), MapPublicIpOnLaunch: awssdk.Bool(public),
		})
		want := rec("subnet-1", TypeSubnet, "subnet-1")
		want.Refs = []inventory.Ref{in(TypeVPC, "vpc-1")}
		want.Zone, want.Tier = "us-east-1a", inventory.TierPrivate
		if public {
			want.Tier = inventory.TierPublic
		}
		want.Attrs = map[string]string{
			"cidr": "10.0.0.0/24", "availability_zone": "us-east-1a", "public": strconv.FormatBool(public),
		}
		check(t, "subnet", subnet, want)
	}

	sg := SecurityGroup(ec2types.SecurityGroup{
		GroupId: s("sg-1"), GroupName: s("web"), VpcId: s("vpc-1"),
		IpPermissions: []ec2types.IpPermission{{
			IpProtocol: s("tcp"), FromPort: awssdk.Int32(443), ToPort: awssdk.Int32(443),
			IpRanges: []ec2types.IpRange{{CidrIp: s("0.0.0.0/0")}},
		}},
		IpPermissionsEgress: []ec2types.IpPermission{{
			IpProtocol: s("-1"), PrefixListIds: []ec2types.PrefixListId{{PrefixListId: s("pl-1")}},
		}},
	})
	want = rec("sg-1", TypeSecurityGroup, "web")
	want.Refs = []inventory.Ref{in(TypeVPC, "vpc-1")}
	want.Rules = []inventory.Rule{
		{
			Protocol:   "tcp",
			FromPort:   awssdk.Int32(443),
			ToPort:     awssdk.Int32(443),
			TargetType: "cidr",
			Target:     "0.0.0.0/0",
		},
		{Egress: true, Protocol: "-1", TargetType: "prefix_list", Target: "pl-1"},
	}
	check(t, "security group", sg, want)

	check(t, "prefix list", PrefixList(ec2types.ManagedPrefixList{
		PrefixListId: s("pl-1"), PrefixListName: s("office"),
	}), rec("pl-1", TypePrefixList, "office"))
}

func TestNamedRecords(t *testing.T) {
	check(t, "KMS key", KMSKey("key-1", "alias/app"), rec("key-1", TypeKMSKey, "alias/app"))
	check(t, "secret", Secret("arn:secret", "app/db"), rec("arn:secret", TypeSecret, "app/db"))
	check(t, "parameter", Parameter("arn:param", "/app/db"), rec("arn:param", TypeParameter, "/app/db"))

	got := Generic("AWS::Lambda::Function", "arn:fn", "fn", "vpc-1", []string{"subnet-b", "subnet-a"}, []string{"sg-1"})
	want := rec("arn:fn", "AWS::Lambda::Function", "fn")
	want.Refs = []inventory.Ref{
		in(TypeSubnet, "subnet-b"), in(TypeSubnet, "subnet-a"), in(TypeVPC, "vpc-1"), memberOf("sg-1"),
	}
	check(t, "generic", got, want)

	if tags := NameTags(""); tags != nil {
		t.Errorf("NameTags(\"\") = %v, want none", tags)
	}
}
