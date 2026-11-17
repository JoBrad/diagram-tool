package aws_sdk

import (
	"slices"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"diagram/internal/inventory"
	"diagram/internal/platform/aws"
)

func name(n string) []ec2types.Tag {
	return []ec2types.Tag{{Key: awssdk.String("Name"), Value: awssdk.String(n)}}
}

// testCollected holds one of each resource the collectors fetch, plus one
// gone resource per type that has a state, so records can skip it.
func testCollected() *collected {
	s := awssdk.String
	return &collected{
		Instances: []ec2types.Instance{
			{
				InstanceId: s("i-1"), Tags: name("web"), VpcId: s("vpc-1"), SubnetId: s("subnet-1"),
				InstanceType: "t3.micro", PlatformDetails: s("Linux/UNIX"),
				PrivateIpAddress: s("10.0.0.5"), PublicIpAddress: s("203.0.113.10"),
				SecurityGroups: []ec2types.GroupIdentifier{{GroupId: s("sg-1")}},
			},
			{InstanceId: s("i-gone"), State: &ec2types.InstanceState{Name: ec2types.InstanceStateNameTerminated}},
		},
		NATGateways: []ec2types.NatGateway{
			{
				NatGatewayId: s("nat-1"), VpcId: s("vpc-1"), SubnetId: s("subnet-1"),
				NatGatewayAddresses: []ec2types.NatGatewayAddress{{PublicIp: s("203.0.113.20")}, {}},
			},
			{NatGatewayId: s("nat-gone"), State: ec2types.NatGatewayStateDeleted},
		},
		VPNGateways: []ec2types.VpnGateway{
			{VpnGatewayId: s("vgw-1"), VpcAttachments: []ec2types.VpcAttachment{
				{VpcId: s("vpc-old"), State: ec2types.AttachmentStatusDetached},
				{VpcId: s("vpc-1"), State: ec2types.AttachmentStatusAttached},
			}},
			{VpnGatewayId: s("vgw-gone"), State: ec2types.VpnStateDeleted},
		},
		InternetGateways: []ec2types.InternetGateway{
			{InternetGatewayId: s("igw-1"), Attachments: []ec2types.InternetGatewayAttachment{
				{VpcId: s("vpc-1"), State: "available"},
			}},
		},
		TransitGateways: []ec2types.TransitGateway{
			{TransitGatewayId: s("tgw-1"), Tags: name("hub")},
			{TransitGatewayId: s("tgw-gone"), State: ec2types.TransitGatewayStateDeleted},
		},
		CustomerGateways: []ec2types.CustomerGateway{
			{CustomerGatewayId: s("cgw-1"), IpAddress: s("198.51.100.1")},
			{CustomerGatewayId: s("cgw-gone"), State: s("deleted")},
		},
		PrefixLists: []ec2types.ManagedPrefixList{{PrefixListId: s("pl-1"), PrefixListName: s("office")}},
		VPCs:        []ec2types.Vpc{{VpcId: s("vpc-1"), CidrBlock: s("10.0.0.0/16")}},
		Subnets: []ec2types.Subnet{{
			SubnetId: s("subnet-1"), VpcId: s("vpc-1"), CidrBlock: s("10.0.0.0/24"),
			AvailabilityZone: s("us-east-1a"), MapPublicIpOnLaunch: awssdk.Bool(true),
		}},
		SecurityGroups: []ec2types.SecurityGroup{{
			GroupId: s("sg-1"), GroupName: s("web"), VpcId: s("vpc-1"),
			IpPermissions: []ec2types.IpPermission{{
				IpProtocol: s("tcp"), FromPort: awssdk.Int32(443), ToPort: awssdk.Int32(443),
				IpRanges: []ec2types.IpRange{{CidrIp: s("0.0.0.0/0")}},
			}},
		}},
		VPNConnections: []ec2types.VpnConnection{
			{
				VpnConnectionId: s("vpn-1"), VpnGatewayId: s("vgw-1"), CustomerGatewayId: s("cgw-1"),
				// Collection scrubs both; records never read them.
				CustomerGatewayConfiguration: s("<vpn_connection>example-psk</vpn_connection>"),
				Options: &ec2types.VpnConnectionOptions{
					TunnelOptions: []ec2types.TunnelOption{{PreSharedKey: s("example-psk")}},
				},
			},
			{VpnConnectionId: s("vpn-2"), TransitGatewayId: s("tgw-1"), CustomerGatewayId: s("cgw-1")},
			{VpnConnectionId: s("vpn-gone"), State: ec2types.VpnStateDeleted},
		},
		KMSKeys: []inventory.Record{aws.KMSKey("key-1", "alias/app")},
	}
}

// TestRecords checks that records builds every resource in a fixed order
// (the order the model draws them in) and skips the ones that are gone. The
// builders' output is tested in package aws.
func TestRecords(t *testing.T) {
	var got []string
	for _, rec := range testCollected().records("us-east-1") {
		got = append(got, rec.ID)
	}
	want := []string{
		"i-1", "nat-1", "vgw-1", "igw-1", "tgw-1", "cgw-1", "pl-1", "vpc-1", "subnet-1", "sg-1",
		"vpn-1", "vpn-2", "key-1",
	}
	if !slices.Equal(got, want) {
		t.Errorf("records = %v, want %v", got, want)
	}
}
